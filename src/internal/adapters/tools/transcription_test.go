package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/transcription"
)

type speechTransport func(*http.Request) (*http.Response, error)

func (f speechTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestWhisperJSONTimingAndMalformedSegments(t *testing.T) {
	valid := []byte(`{"transcription":[{"offsets":{"from":1250,"to":4000},"text":" actual speech "}]}`)
	track, err := ParseWhisper(valid, 60)
	if err != nil || track.Cues[0].Start != 1.25 || track.Cues[0].Text != "actual speech" {
		t.Fatal(track, err)
	}
	for _, raw := range []string{`{"transcription":[]}`, `{"transcription":[{"offsets":{"from":-1,"to":1000},"text":"bad"}]}`, `{"transcription":[{"offsets":{"from":0,"to":61000},"text":"bad"}]}`, `{"transcription":[{"offsets":{"from":1000,"to":2000},"text":"a"},{"offsets":{"from":500,"to":3000},"text":"b"}]}`} {
		if _, err := ParseWhisper([]byte(raw), 60); err == nil {
			t.Fatal("invalid timing accepted")
		}
	}
}
func TestChineseRecognitionIsSimplifiedWithoutChangingOtherLanguages(t *testing.T) {
	for _, test := range []struct{ selected, detected, want string }{
		{"zh", "zh", "听说这个视频里面有中文，学习笔记。"},
		{"zh", "", "听说这个视频里面有中文，学习笔记。"},
		{"auto", "zh", "听说这个视频里面有中文，学习笔记。"},
		{"auto", "ja", "聽說這個視頻裡面有中文，學習筆記。"},
		{"en", "en", "聽說這個視頻裡面有中文，學習筆記。"},
	} {
		raw := []byte(`{"result":{"language":"` + test.detected + `"},"transcription":[{"offsets":{"from":1250,"to":4000},"text":"聽說這個視頻裡面有中文，學習筆記。"}]}`)
		track, err := parseWhisper(raw, 60, test.selected)
		if err != nil || track.Cues[0].Text != test.want || track.Cues[0].Start != 1.25 || track.Cues[0].End != 4 {
			t.Fatal(test, track, err)
		}
	}
}
func TestRealLocalSpeechPipeline(t *testing.T) {
	model := os.Getenv("TANGENTRY_WHISPER_SMOKE_MODEL")
	sample := os.Getenv("TANGENTRY_WHISPER_SMOKE_AUDIO")
	if model == "" || sample == "" {
		t.Skip("explicit local model and short sample required")
	}
	bytes, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Transcriber{FFmpeg: "ffmpeg", HTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credential leaked")
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(bytes)), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(bytes)))}, nil
	})}}
	cfg := transcription.Config{Backend: os.Getenv("TANGENTRY_WHISPER_SMOKE_BACKEND"), Python: os.Getenv("TANGENTRY_WHISPER_SMOKE_PYTHON"), Binary: "whisper-cli", Model: model, Language: "en"}
	for _, local := range []bool{false, true} {
		audio := transcription.Audio{URL: "https://speech-fixture.example/jfk.wav", Duration: 11}
		firstStage := "fetching"
		if local {
			audio.LocalPath, firstStage = sample, "local"
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		stages := []string{}
		track, err := engine.Transcribe(ctx, cfg, audio, 60, func(s string) { stages = append(stages, s) })
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		words := ""
		for _, cue := range track.Cues {
			words += strings.ToLower(cue.Text)
		}
		if !strings.Contains(words, "country") || len(stages) != 3 || stages[0] != firstStage || stages[2] != "recognizing" {
			t.Fatal("real conversion/recognition did not produce speech", stages)
		}
		if local {
			remaining, err := os.ReadFile(sample)
			if err != nil || string(remaining) != string(bytes) {
				t.Fatal("persistent source changed", err)
			}
		}
	}
}

func TestInstalledModelsAreSeparatedByEngine(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(root+"/ggml-base.bin", []byte("model fixture"), 0600)
	os.Mkdir(root+"/faster-tiny", 0700)
	for _, file := range []string{"model.bin", "config.json", "tokenizer.json"} {
		os.WriteFile(root+"/faster-tiny/"+file, []byte("fixture"), 0600)
	}
	os.Mkdir(root+"/incomplete", 0700)
	os.WriteFile(root+"/incomplete/model.bin", []byte("fixture"), 0600)
	engine := &Transcriber{ModelDir: root}
	models := engine.models(transcription.Config{})
	if len(models) != 2 || models[0].Backend == models[1].Backend {
		t.Fatal("models mixed or incomplete directory accepted", models)
	}
	if !fasterModel(root+"/faster-tiny") || fasterModel(root+"/ggml-base.bin") {
		t.Fatal("incompatible model accepted")
	}
}

func TestQwenModelDirectoriesAreValidatedAndEnumeratedLocally(t *testing.T) {
	root := t.TempDir()
	for name, timestamp := range map[string]bool{"asr": false, "aligner": true} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		config := `{"model_type":"qwen3_asr"}`
		if timestamp {
			config = `{"model_type":"qwen3_asr","timestamp_token_id":42}`
		}
		for file, data := range map[string]string{
			"config.json": config, "tokenizer_config.json": "{}", "preprocessor_config.json": "{}", "model.safetensors": "weights", "vocab.json": "{}", "merges.txt": "#version: 0.2",
		} {
			if err := os.WriteFile(filepath.Join(path, file), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !qwenModel(filepath.Join(root, "asr"), false) || !qwenModel(filepath.Join(root, "aligner"), true) || qwenModel(filepath.Join(root, "asr"), true) {
		t.Fatal("Qwen model validation did not distinguish ASR and aligner")
	}
	models := (&Transcriber{ModelDir: root}).models(transcription.Config{})
	if len(models) != 1 || models[0].Backend != "qwen3-asr" {
		t.Fatal("local Qwen ASR was not listed", models)
	}
}

func TestQwenShardedModelRejectsOversizeMetadataAndMissingWeights(t *testing.T) {
	path := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := `{"model_type":"qwen3_asr"}`
	index := `{"weight_map":{"layer":"model-00001.safetensors"}}`
	write("config.json", config)
	write("tokenizer_config.json", "{}")
	write("preprocessor_config.json", "{}")
	write("vocab.json", "{}")
	write("merges.txt", "#version: 0.2")
	write("model.safetensors.index.json", index)
	write("model-00001.safetensors", "fixture weights")
	if !qwenModel(path, false) {
		t.Fatal("valid sharded model rejected")
	}
	for _, name := range []string{"vocab.json", "merges.txt"} {
		if err := os.Rename(filepath.Join(path, name), filepath.Join(path, name+".backup")); err != nil {
			t.Fatal(err)
		}
		if qwenModel(path, false) {
			t.Fatal("model missing tokenizer files accepted", name)
		}
		if err := os.Rename(filepath.Join(path, name+".backup"), filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"config.json", "model.safetensors.index.json"} {
		value := config
		if field != "config.json" {
			value = index
		}
		write(field, value+strings.Repeat(" ", 1<<20))
		if qwenModel(path, false) {
			t.Fatal("oversize model metadata accepted", field)
		}
		write(field, value)
	}
	write("model.safetensors.index.json", `{"weight_map":{"layer":"../outside.safetensors"}}`)
	if qwenModel(path, false) {
		t.Fatal("weight shard escaped model directory")
	}
	write("model.safetensors.index.json", index)
	if err := os.Remove(filepath.Join(path, "model-00001.safetensors")); err != nil {
		t.Fatal(err)
	}
	if qwenModel(path, false) {
		t.Fatal("incomplete sharded model accepted")
	}
}

func TestQwenHelperUsesOfflineModelsAndRealAlignmentOffsets(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required")
	}
	root := t.TempDir()
	files := map[string][]byte{
		"recognize.py": qwenScript,
		"torch.py":     []byte("float32='float32'\ndef set_num_threads(value): assert value == 4\n"),
		"numpy.py":     []byte("float32='float32'\nclass Samples:\n def __init__(self,n): self.n=n\n def __len__(self): return self.n\n def astype(self,dtype): return self\n def __truediv__(self,value): return self\ndef frombuffer(data,dtype): return Samples(len(data)//2)\ndef abs(value): return value\ndef max(value): return .1\n"),
		"qwen_asr.py": []byte(`import os
from types import SimpleNamespace as S
class Qwen3ASRModel:
 @classmethod
 def from_pretrained(cls,path,**options):
  assert os.environ['HF_HUB_OFFLINE']=='1' and os.environ['TRANSFORMERS_OFFLINE']=='1'
  assert options['local_files_only'] and options['forced_aligner_kwargs']['local_files_only']
  assert options['device_map']=='cpu' and options['max_inference_batch_size']==1
  return cls()
 def transcribe(self,audio,**options):
  assert options['return_time_stamps'] and options['language']=='Chinese'
  return [S(language='Chinese',text='你好',time_stamps=S(items=[S(text='你',start_time=.5,end_time=1),S(text='好',start_time=2,end_time=2.5)]))]
`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wav := filepath.Join(root, "audio.wav")
	for _, test := range []struct {
		name, audio string
		offset      float64
	}{
		{"steady", "(b'\\x0b\\x00')*(33*16000)", 30},
		{"pause", "(b'\\x0b\\x00')*(288*1600)+b'\\0\\0'*(2*1600)+(b'\\x0b\\x00')*(4*16000)", 28.95},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepare := command(ctx, python, "-c", "import wave,sys\nwith wave.open(sys.argv[1],'wb') as f:\n f.setparams((1,2,16000,0,'NONE','not compressed'))\n f.writeframes("+test.audio+")", wav)
			if out, err := prepare.CombinedOutput(); err != nil {
				t.Fatal(err, string(out))
			}
			output := filepath.Join(root, "output.json")
			cmd := command(ctx, python, filepath.Join(root, "recognize.py"), root, wav, "zh", output, root)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(err, string(out))
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			track, err := parseWhisper(data, 0, "zh")
			if err != nil || len(track.Cues) != 4 || track.Cues[0].Start != .5 || track.Cues[2].Start != test.offset+.5 || track.Cues[3].End != test.offset+2.5 {
				t.Fatal(track, err)
			}
		})
	}
}

func TestQwenAudioCutsPreserveSamplesAndExactOffsets(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required")
	}
	helper := filepath.Join(t.TempDir(), "helper.py")
	if err := os.WriteFile(helper, qwenScript, 0600); err != nil {
		t.Fatal(err)
	}
	check := `import io,runpy,sys,wave
blocks=runpy.run_path(sys.argv[1])['audio_blocks']
rate=16000
def split(raw):
 buffer=io.BytesIO()
 with wave.open(buffer,'wb') as writer:
  writer.setparams((1,2,rate,0,'NONE','not compressed'))
  writer.writeframes(raw)
 buffer.seek(0)
 with wave.open(buffer,'rb') as reader:
  result=list(blocks(reader))
 assert b''.join(part for part,offset in result)==raw, 'lost or repeated samples'
 position=0
 for part,offset in result:
  assert offset==position/rate, (offset,position)
  assert 0<len(part)<=30*rate*2
  position+=len(part)//2
 return result
sound=b'\xe8\x03'
raw=sound*(29*rate)+b'\0\0'*(rate//5)+sound*(36*rate+123)
result=split(raw)
assert 29<=len(result[0][0])/(rate*2)<=29.2, 'speech cut instead of quiet interval'
assert result[1][1]==len(result[0][0])/(rate*2)
steady=split(sound*(65*rate+123))
assert [offset for part,offset in steady]==[0,30,60]
silent=split(b'\0\0'*(65*rate))
assert [offset for part,offset in silent]==[0,30,60]
assert split(b'')==[]
assert len(split(sound*123))==1
assert len(split(sound*(30*rate)))==1
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := command(ctx, python, "-c", check, helper).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

func TestQwenAlignmentPreservesOriginalPunctuationAndSpaces(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required")
	}
	root := t.TempDir()
	helper := filepath.Join(root, "helper.py")
	if err := os.WriteFile(helper, qwenScript, 0600); err != nil {
		t.Fatal(err)
	}
	check := `import runpy,sys
from types import SimpleNamespace as S
parts=runpy.run_path(sys.argv[1])['aligned_parts']
items=[S(text='Hello',start_time=.5,end_time=1),S(text='world',start_time=1.1,end_time=2)]
result=parts(items,3,'Hello, world!')
assert result==[(.5,2,'Hello, world!')], result
items=[S(text='你',start_time=.5,end_time=1),S(text='好',start_time=2,end_time=2.5)]
assert ''.join(p[2] for p in parts(items,3,'你好！'))=='你好！'
for text in ['你好！还有内容','不匹配']:
 try: parts(items,3,text)
 except ValueError: pass
 else: raise AssertionError('alignment mismatch accepted')
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := command(ctx, python, "-c", check, helper).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

func TestFasterWhisperTimestampsNeverExtendPastRealAudioSamples(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required for explicit process fixture")
	}
	root := t.TempDir()
	files := map[string][]byte{
		"recognize.py":      fasterScript,
		"numpy.py":          []byte("float32='f4'\nclass Samples:\n size=1\n def astype(self, dtype): return self\n def __truediv__(self, value): return self\ndef abs(value): return value\ndef max(value): return 0.001\ndef frombuffer(data, dtype): return Samples()\n"),
		"faster_whisper.py": []byte("from types import SimpleNamespace as S\nclass WhisperModel:\n def __init__(self, path, **options): assert options['local_files_only'] is True\n def transcribe(self, samples, **options):\n  assert options['word_timestamps'] is True\n  return iter([S(start=.125,end=.5,text='inside'),S(start=58,end=63,text='real tail'),S(start=60,end=65,text='padding'),S(start=61,end=70,text='more padding')]),S(language='zh')\n"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	wav := filepath.Join(root, "audio.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prepare := command(ctx, python, "-c", "import wave,sys\nwith wave.open(sys.argv[1],'wb') as f:\n f.setparams((1,2,16000,0,'NONE','not compressed'))\n f.writeframes((b'\\x0b\\x00')*(60*16000))", wav)
	if output, err := prepare.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	output := filepath.Join(root, "transcript.json")
	cmd := command(ctx, python, filepath.Join(root, "recognize.py"), root, wav, "zh", output)
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(result))
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	track, err := parseWhisper(data, 60, "zh")
	if err != nil || len(track.Cues) != 2 {
		t.Fatal("padded timestamps invalidated an otherwise valid preview", track, err)
	}
	if track.Cues[0].Start != .125 || track.Cues[0].End != .5 || track.Cues[1].Start != 58 || track.Cues[1].End != 60 {
		t.Fatal("real cue times changed or padding retained", track)
	}
}

func TestFasterWhisperRejectsOnlyNearDigitalSilence(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required for explicit process fixture")
	}
	root := t.TempDir()
	for name, data := range map[string][]byte{
		"recognize.py":      fasterScript,
		"numpy.py":          []byte("float32='f4'\nclass Samples:\n size=1\n def astype(self, dtype): return self\n def __truediv__(self, value): return self\ndef abs(value): return value\ndef max(value): return 0\ndef frombuffer(data, dtype): return Samples()\n"),
		"faster_whisper.py": []byte("class WhisperModel:\n def __init__(self, path, **options): raise AssertionError('silence should be rejected before model load')\n"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	wav := filepath.Join(root, "silent.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prepare := command(ctx, python, "-c", "import wave,sys\nwith wave.open(sys.argv[1],'wb') as f:\n f.setparams((1,2,16000,0,'NONE','not compressed'))\n f.writeframes(b'\\0'*(16000*2))", wav)
	if output, err := prepare.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	cmd := command(ctx, python, filepath.Join(root, "recognize.py"), root, wav, "zh", filepath.Join(root, "transcript.json"))
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "no speech") {
		t.Fatal("silent input reached recognizer", err, string(output))
	}
}

func TestFasterWhisperShortCuesUseAlignedWordsWithoutLosingText(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "recognize.py")
	if err := os.WriteFile(helper, fasterScript, 0600); err != nil {
		t.Fatal(err)
	}
	check := `import ast, math, sys
from pathlib import Path
from types import SimpleNamespace as S
tree = ast.parse(Path(sys.argv[1]).read_text())
fn = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'subtitle_parts')
exec(compile(ast.Module(body=[fn], type_ignores=[]), '<fixture>', 'exec'))
def segment(words, text=None):
 return S(start=0, end=30, text=text if text is not None else ''.join(w.word for w in words), words=words)
words = [S(start=1, end=2, word='第一句。'), S(start=4, end=5, word=' 第二句。')]
assert subtitle_parts(segment(words)) == [(1,2,'第一句。'),(4,5,' 第二句。')]
words = [S(start=i, end=i+.5, word='字') for i in range(30)]
parts = subtitle_parts(segment(words))
assert len(parts) > 1 and ''.join(p[2] for p in parts) == '字'*30
assert all(p[1]-p[0] <= 8 for p in parts)
assert all(p[0] in [w.start for w in words] and p[1] in [w.end for w in words] for p in parts)
words = [S(start=1, end=2, word='a'),S(start=4,end=5,word=' b')]
assert subtitle_parts(segment(words)) == [(1,2,'a'),(4,5,' b')]
for words in [None, [], [S(start=0,end=1,word='missing')], [S(start=float('nan'),end=1,word='kept')], [S(start=2,end=1,word='kept')]]:
 value = segment(words or [], 'kept')
 value.words = words
 assert subtitle_parts(value) == [(0,30,'kept')]
words = [S(start=0,end=0,word='a'), S(start=0,end=0,word='b')]
assert subtitle_parts(segment(words)) == [(0,30,'ab')]
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := command(ctx, executable("", "python3"), "-c", check, helper)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
}
