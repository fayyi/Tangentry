package tools

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tangentry/internal/transcription"
)

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}
func fasterModel(path string) bool {
	return regularFile(filepath.Join(path, "model.bin")) && regularFile(filepath.Join(path, "config.json")) && regularFile(filepath.Join(path, "tokenizer.json"))
}

func modelJSON(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, transcription.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, transcription.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, transcription.ErrInvalid
	}
	return data, nil
}

func qwenModel(path string, aligner bool) bool {
	data, err := modelJSON(filepath.Join(path, "config.json"))
	if err != nil {
		return false
	}
	var config struct {
		ModelType        string `json:"model_type"`
		TimestampTokenID *int   `json:"timestamp_token_id"`
	}
	if json.Unmarshal(data, &config) != nil || config.ModelType != "qwen3_asr" || (config.TimestampTokenID != nil) != aligner {
		return false
	}
	if !regularFile(filepath.Join(path, "tokenizer_config.json")) || !regularFile(filepath.Join(path, "preprocessor_config.json")) {
		return false
	}
	if !regularFile(filepath.Join(path, "vocab.json")) || !regularFile(filepath.Join(path, "merges.txt")) {
		return false
	}
	if regularFile(filepath.Join(path, "model.safetensors")) {
		return true
	}
	data, err = modelJSON(filepath.Join(path, "model.safetensors.index.json"))
	if err != nil {
		return false
	}
	var index struct {
		WeightMap map[string]string `json:"weight_map"`
	}
	if json.Unmarshal(data, &index) != nil || len(index.WeightMap) == 0 {
		return false
	}
	for _, shard := range index.WeightMap {
		if filepath.Base(shard) != shard || !strings.HasSuffix(shard, ".safetensors") || !regularFile(filepath.Join(path, shard)) {
			return false
		}
	}
	return true
}
func (t *Transcriber) models(c transcription.Config) []transcription.Model {
	roots := []string{t.ModelDir}
	if info, err := os.Stat(c.Model); err == nil {
		if info.IsDir() {
			roots = append(roots, filepath.Dir(c.Model))
		} else {
			roots = append(roots, filepath.Dir(c.Model))
		}
	}
	seen := map[string]bool{}
	models := []transcription.Model{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		files, _ := os.ReadDir(root)
		for _, file := range files {
			path := filepath.Join(root, file.Name())
			if seen[path] {
				continue
			}
			backend, label := "", ""
			if !file.IsDir() && strings.HasPrefix(file.Name(), "ggml-") && strings.HasSuffix(file.Name(), ".bin") && regularFile(path) {
				backend = "whisper.cpp"
				label = strings.TrimSuffix(strings.TrimPrefix(file.Name(), "ggml-"), ".bin")
			}
			if file.IsDir() && fasterModel(path) {
				backend = "faster-whisper"
				label = strings.TrimPrefix(file.Name(), "faster-")
			}
			if file.IsDir() && qwenModel(path, false) {
				backend, label = "qwen3-asr", file.Name()
			}
			if backend != "" {
				models = append(models, transcription.Model{Backend: backend, Label: label, Path: path})
				seen[path] = true
			}
			if len(models) >= 32 {
				break
			}
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Backend+models[i].Label < models[j].Backend+models[j].Label })
	return models
}
func fasterInstalled(binary string) bool {
	return pythonModuleInstalled(binary, "faster_whisper")
}

func qwenInstalled(binary string) bool {
	python := executable(binary, "python3")
	if python == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Import checks the actual runtime dependency chain, without loading weights.
	cmd := command(ctx, python, "-c", "import os; os.environ['HF_HUB_OFFLINE']='1'; os.environ['TRANSFORMERS_OFFLINE']='1'; from qwen_asr import Qwen3ASRModel; import torch")
	cmd.Env = toolEnvironment()
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run() == nil
}
func pythonModuleInstalled(binary, module string) bool {
	python := executable(binary, "python3")
	if python == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := command(ctx, python, "-c", "import importlib.util, sys; raise SystemExit(0 if importlib.util.find_spec(sys.argv[1]) else 1)", module)
	cmd.Env = toolEnvironment()
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run() == nil
}
