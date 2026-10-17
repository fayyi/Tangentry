package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"tangentry/internal/transcription"
	"testing"
)

func TestModelInstallerPublishesOnlyCompleteDownloads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	for _, complete := range []bool{false, true} {
		root := t.TempDir()
		python := filepath.Join(t.TempDir(), "fixture-python")
		script := "#!/bin/sh\nif [ \"$1\" = \"-c\" ]; then exit 0; fi\nmkdir -p \"$3\"\nprintf fixture > \"$3/model.bin\"\n"
		if complete {
			script += "printf '{}' > \"$3/config.json\"\nprintf '{}' > \"$3/tokenizer.json\"\nprintf '{}' > \"$3/tangentry-source.json\"\n"
		}
		if err := os.WriteFile(python, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		installer := &ModelInstaller{Root: root}
		var option transcription.ModelOption
		for _, candidate := range transcription.ModelCatalog() {
			if candidate.ID == "small" {
				option = candidate
			}
		}
		if !installer.Available(python) {
			t.Fatal("fixture unavailable")
		}
		err := installer.Install(context.Background(), option, python, func(string) {})
		path, installed := installer.Installed(option)
		if installed != complete || (err == nil) != complete {
			t.Fatal(complete, installed, err)
		}
		files, _ := os.ReadDir(root)
		for _, file := range files {
			if file.Name() != filepath.Base(path) {
				t.Fatal("temporary download leaked", file.Name())
			}
		}
	}
}

func TestQwenDownloadsShareWhisperRootAndPublishCompleteModels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	for _, option := range transcription.ModelCatalog() {
		if option.Backend != "qwen3-asr" {
			continue
		}
		t.Run(option.ID, func(t *testing.T) {
			root := t.TempDir()
			python := filepath.Join(t.TempDir(), "fixture-python")
			config := `{"model_type":"qwen3_asr"}`
			if option.ID == "qwen3-aligner-0.6b" {
				config = `{"model_type":"qwen3_asr","timestamp_token_id":42}`
			}
			script := "#!/bin/sh\nmkdir -p \"$3\"\nprintf '%s' '" + config + "' > \"$3/config.json\"\nprintf '{}' > \"$3/tokenizer_config.json\"\nprintf '{}' > \"$3/preprocessor_config.json\"\nprintf '{}' > \"$3/vocab.json\"\nprintf merges > \"$3/merges.txt\"\nprintf weights > \"$3/model.safetensors\"\nprintf '{}' > \"$3/tangentry-source.json\"\n"
			if err := os.WriteFile(python, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			installer := &ModelInstaller{Root: root}
			if err := installer.Install(context.Background(), option, python, func(string) {}); err != nil {
				t.Fatal(err)
			}
			path, ok := installer.Installed(option)
			if !ok || path != filepath.Join(root, option.ID) {
				t.Fatal("Qwen installed outside shared model root", path, ok)
			}
			for _, name := range []string{"vocab.json", "merges.txt"} {
				original := filepath.Join(path, name)
				if err := os.Rename(original, original+".backup"); err != nil {
					t.Fatal(err)
				}
				if _, installed := installer.Installed(option); installed {
					t.Fatal("incomplete tokenizer reported installed", name)
				}
				if err := os.Rename(original+".backup", original); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestIncompleteQwenDownloadPreservesExistingModelAndCleansStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	var option transcription.ModelOption
	for _, candidate := range transcription.ModelCatalog() {
		if candidate.ID == "qwen3-asr-0.6b" {
			option = candidate
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, option.ID)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"config.json": `{"model_type":"qwen3_asr"}`, "tokenizer_config.json": "{}", "preprocessor_config.json": "{}",
		"vocab.json": "{}", "merges.txt": "original merges", "model.safetensors": "original weights",
	} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	python := filepath.Join(t.TempDir(), "fixture-python")
	script := "#!/bin/sh\nmkdir -p \"$3\"\nprintf '%s' '{\"model_type\":\"qwen3_asr\"}' > \"$3/config.json\"\nprintf '{}' > \"$3/tokenizer_config.json\"\nprintf '{}' > \"$3/preprocessor_config.json\"\nprintf '{}' > \"$3/vocab.json\"\nprintf replacement > \"$3/model.safetensors\"\nprintf '{}' > \"$3/tangentry-source.json\"\n"
	if err := os.WriteFile(python, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	installer := &ModelInstaller{Root: root}
	if err := installer.Install(context.Background(), option, python, func(string) {}); err != transcription.ErrUnavailable {
		t.Fatal("incomplete download not rejected", err)
	}
	if _, installed := installer.Installed(option); !installed {
		t.Fatal("existing model lost")
	}
	weights, err := os.ReadFile(filepath.Join(path, "model.safetensors"))
	if err != nil || string(weights) != "original weights" {
		t.Fatal("existing weights replaced", string(weights), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != option.ID {
		t.Fatal("staging directory leaked", entries, err)
	}
}

func TestModelDownloadHelperRequiresOnlyHubAndPinsRevision(t *testing.T) {
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "download.py"), modelDownloadScript, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `from pathlib import Path
from types import SimpleNamespace
class HfApi:
 def __init__(self,token): assert token is False
 def model_info(self,repository): return SimpleNamespace(sha='pinned-fixture-revision')
def snapshot_download(repository,**options):
 assert options['revision']=='pinned-fixture-revision'
 assert options['token'] is False and options['max_workers']==2
 patterns=options['allow_patterns']
 assert ('*.safetensors' in patterns)==repository.startswith('Qwen/')
 assert ('model.bin' in patterns)==(not repository.startswith('Qwen/'))
 Path(options['local_dir']).mkdir()
`
	if err := os.WriteFile(filepath.Join(root, "huggingface_hub.py"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"", "qwen3-asr"} {
		repo := "Systran/faster-whisper-base"
		if backend != "" {
			repo = "Qwen/Qwen3-ASR-0.6B"
		}
		destination := filepath.Join(root, "model-"+backend)
		if out, err := command(context.Background(), python, filepath.Join(root, "download.py"), repo, destination, backend).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		if !regularFile(filepath.Join(destination, "tangentry-source.json")) {
			t.Fatal("provenance missing")
		}
	}
}

func TestModelDownloadRepairsIncompleteDirectoryWithoutDeletingOldFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	for _, id := range []string{"small", "qwen3-asr-0.6b", "qwen3-aligner-0.6b"} {
		t.Run(id, func(t *testing.T) {
			var option transcription.ModelOption
			for _, candidate := range transcription.ModelCatalog() {
				if candidate.ID == id {
					option = candidate
				}
			}
			root := t.TempDir()
			installer := &ModelInstaller{Root: root}
			target, _ := installer.Installed(option)
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "user-file"), []byte("keep original"), 0600); err != nil {
				t.Fatal(err)
			}
			python := filepath.Join(t.TempDir(), "fixture-python")
			script := "#!/bin/sh\nmkdir -p \"$3\"\nprintf '{}' > \"$3/tangentry-source.json\"\n"
			if option.Backend == "qwen3-asr" {
				config := `{"model_type":"qwen3_asr"}`
				if id == "qwen3-aligner-0.6b" {
					config = `{"model_type":"qwen3_asr","timestamp_token_id":42}`
				}
				script += "printf '%s' '" + config + "' > \"$3/config.json\"\nprintf '{}' > \"$3/tokenizer_config.json\"\nprintf '{}' > \"$3/preprocessor_config.json\"\nprintf '{}' > \"$3/vocab.json\"\nprintf merges > \"$3/merges.txt\"\nprintf weights > \"$3/model.safetensors\"\n"
			} else {
				script += "printf weights > \"$3/model.bin\"\nprintf '{}' > \"$3/config.json\"\nprintf '{}' > \"$3/tokenizer.json\"\n"
			}
			if err := os.WriteFile(python, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := installer.Install(context.Background(), option, python, func(string) {}); err != nil {
				t.Fatal("retry failed", err)
			}
			if _, installed := installer.Installed(option); !installed {
				t.Fatal("complete model not published")
			}
			backups, err := filepath.Glob(filepath.Join(root, ".model-backup-*", "previous", "user-file"))
			if err != nil || len(backups) != 1 {
				t.Fatal("old files not retained", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil || string(data) != "keep original" {
				t.Fatal("backup altered", string(data), err)
			}
			staging, err := filepath.Glob(filepath.Join(root, ".model-download-*"))
			if err != nil || len(staging) != 0 {
				t.Fatal("staging leaked", staging, err)
			}
		})
	}
}

func TestModelPublicationRestoresIncompleteDirectoryWhenRenameFails(t *testing.T) {
	root := t.TempDir()
	installer := &ModelInstaller{Root: root}
	option := transcription.ModelOption{ID: "small"}
	target, _ := installer.Installed(option)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "user-file")
	if err := os.WriteFile(marker, []byte("keep original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installer.publish(context.Background(), filepath.Join(root, "missing-source"), option); err == nil {
		t.Fatal("missing source accepted")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep original" {
		t.Fatal("original not restored", string(data), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(target) {
		t.Fatal("empty backup leaked", entries, err)
	}
}

func TestModelPublicationPreservesWorkingModelAddedDuringDownload(t *testing.T) {
	root := t.TempDir()
	installer := &ModelInstaller{Root: root}
	option := transcription.ModelOption{ID: "small"}
	target, _ := installer.Installed(option)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model.bin", "config.json", "tokenizer.json"} {
		if err := os.WriteFile(filepath.Join(target, name), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := installer.publish(context.Background(), filepath.Join(root, "missing-source"), option); err != nil {
		t.Fatal("working model was replaced", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "model.bin"))
	if err != nil || string(data) != "original" {
		t.Fatal("working weights changed", string(data), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := installer.publish(ctx, filepath.Join(root, "missing-source"), option); err != context.Canceled {
		t.Fatal("canceled publication continued", err)
	}
}
