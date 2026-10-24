package tools

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseWhisperServerVerboseJSON(t *testing.T) {
	track, err := parseWhisperServer([]byte(`{"language":"chinese","segments":[{"start":0.25,"end":1.5,"text":" 聽見這句話。 "},{"start":2,"end":3.25,"text":"下一句"}]}`), "zh")
	if err != nil {
		t.Fatal(err)
	}
	if track.Source != "automatic" || !track.Automatic || track.Label != "简体中文 · 实时本机字幕" || len(track.Cues) != 2 {
		t.Fatalf("unexpected track: %#v", track)
	}
	if track.Cues[0].Text != "听见这句话。" || track.Cues[1].Start != 2 {
		t.Fatalf("unexpected cues: %#v", track.Cues)
	}
}

func TestParseWhisperServerRejectsInvalidCues(t *testing.T) {
	for _, raw := range []string{
		`{"segments":[{"start":-1,"end":1,"text":"bad"}]}`,
		`{"segments":[{"start":2,"end":1,"text":"bad"}]}`,
		`{"segments":[{"start":2,"end":3,"text":"a"},{"start":1,"end":2,"text":"b"}]}`,
		`{"segments":[{"start":0,"end":1,"text":" "}]}`,
	} {
		if _, err := parseWhisperServer([]byte(raw), "en"); err == nil {
			t.Fatalf("invalid server response accepted: %s", raw)
		}
	}
}

func TestWritePCM16ProducesCanonicalWAV(t *testing.T) {
	pcm := []byte{0, 0, 0xff, 0x7f}
	var output bytes.Buffer
	if err := writePCM16(&output, pcm, 16000, 1); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	if len(data) != 48 || string(data[:4]) != "RIFF" || string(data[8:16]) != "WAVEfmt " || string(data[36:40]) != "data" {
		t.Fatalf("unexpected WAV framing: %q (%d bytes)", data[:min(len(data), 40)], len(data))
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("data size = %d, want %d", got, len(pcm))
	}
}

func TestParseWhisperServerLeavesEnglishUntouched(t *testing.T) {
	track, err := parseWhisperServer([]byte(`{"language":"english","segments":[{"start":0,"end":1,"text":"聽見"}]}`), "auto")
	if err != nil || track.Cues[0].Text != "聽見" {
		t.Fatalf("track changed for non-Chinese language: %#v, %v", track, err)
	}
}

func TestParseWhisperServerAllowsSilence(t *testing.T) {
	track, err := parseWhisperServer([]byte(`{"language":"zh","segments":[]}`), "zh")
	if err != nil {
		t.Fatal(err)
	}
	if len(track.Cues) != 0 || !track.Automatic || track.Source != "automatic" {
		t.Fatalf("silence should produce an empty streaming track: %#v", track)
	}
}
