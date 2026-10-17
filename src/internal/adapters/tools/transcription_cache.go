package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

const speechCacheBytes = 64 << 20
const speechCacheFiles = 2000
const speechCacheEntryBytes = 4 << 20

type speechCacheEntry struct {
	size int64
	used time.Time
}
type speechCache struct {
	root    string
	entries map[string]speechCacheEntry
	bytes   int64
}
type cachedSpeech struct {
	Version int             `json:"version"`
	Key     string          `json:"key"`
	Cues    []subtitles.Cue `json:"cues"`
}

func cacheFailure() error {
	return transcription.Failed("transcription_cache", "无法读写转录片段缓存，请检查数据目录权限与磁盘空间后重试。")
}

func newSpeechCache(root string) (*speechCache, error) {
	if root == "" {
		return nil, nil
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, cacheFailure()
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, cacheFailure()
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, cacheFailure()
	}
	files, err := os.ReadDir(root)
	if err != nil {
		return nil, cacheFailure()
	}
	cache := &speechCache{root: root, entries: make(map[string]speechCacheEntry)}
	for _, file := range files {
		key := strings.TrimSuffix(file.Name(), ".json")
		if len(key) != 64 || file.Name() != key+".json" {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		cache.entries[key] = speechCacheEntry{size: info.Size(), used: info.ModTime()}
		cache.bytes += info.Size()
	}
	if err := cache.prune(); err != nil {
		return nil, err
	}
	return cache, nil
}

func speechCacheKey(ctx context.Context, cfg transcription.Config, chunk speechChunk, duration float64) (string, error) {
	model := "FunAudioLLM/SenseVoiceSmall"
	if cfg.Backend == "siliconflow" && cfg.SiliconflowModel != "" {
		model = cfg.SiliconflowModel
	}
	if cfg.Backend == "mai" {
		model = "mai-transcribe-2"
	}
	if cfg.Backend == "groq" {
		model = cfg.GroqModel
		if model == "" {
			model = "whisper-large-v3-turbo"
		}
	} else if cfg.Backend == "cloudflare" {
		model = "@cf/openai/whisper-large-v3-turbo"
	}
	// Recognition settings and the actual compressed bytes determine reuse.
	// Account keys and expiring source URLs are deliberately excluded.
	spec, err := json.Marshal(struct {
		Version                  int
		Backend, Model, Language string
		Duration                 float64
	}{1, cfg.Backend, model, cfg.Language, duration})
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write(spec)
	file, err := os.Open(chunk.path)
	if err != nil {
		return "", storageFailure(err)
	}
	defer file.Close()
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buffer)
		hash.Write(buffer[:n])
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", storageFailure(readErr)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validCachedSpeech(cues []subtitles.Cue, duration float64) bool {
	if len(cues) > 10000 {
		return false
	}
	var previous float64 = -1
	total := 0
	for _, cue := range cues {
		if math.IsNaN(cue.Start) || math.IsNaN(cue.End) || math.IsInf(cue.Start, 0) || math.IsInf(cue.End, 0) || cue.Start < 0 || cue.Start < previous || cue.End <= cue.Start || cue.End > duration+.001 || !utf8.ValidString(cue.Text) || strings.TrimSpace(cue.Text) == "" || len(cue.Text) > 8000 {
			return false
		}
		total += len(cue.Text)
		if total > 2<<20 {
			return false
		}
		previous = cue.Start
	}
	return true
}

func (cache *speechCache) load(key string, duration float64) ([]subtitles.Cue, bool, error) {
	if cache == nil {
		return nil, false, nil
	}
	path := filepath.Join(cache.root, key+".json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, cacheFailure()
	}
	if !info.Mode().IsRegular() || info.Size() > speechCacheEntryBytes {
		return nil, false, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, cacheFailure()
	}
	data, err := io.ReadAll(io.LimitReader(file, speechCacheEntryBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, false, cacheFailure()
	}
	var record cachedSpeech
	if len(data) > speechCacheEntryBytes || json.Unmarshal(data, &record) != nil || record.Version != 1 || record.Key != key || record.Cues == nil || !validCachedSpeech(record.Cues, duration) {
		return nil, false, nil
	}
	now := time.Now()
	if entry, ok := cache.entries[key]; ok {
		entry.used = now
		cache.entries[key] = entry
	}
	_ = os.Chtimes(path, now, now)
	return record.Cues, true, nil
}

func (cache *speechCache) save(key string, cues []subtitles.Cue, duration float64) error {
	if cache == nil {
		return nil
	}
	if !validCachedSpeech(cues, duration) {
		return subtitles.ErrFormat
	}
	if cues == nil {
		cues = []subtitles.Cue{}
	}
	data, err := json.Marshal(cachedSpeech{Version: 1, Key: key, Cues: cues})
	if err != nil || len(data) > speechCacheEntryBytes {
		return subtitles.ErrFormat
	}
	file, err := os.CreateTemp(cache.root, ".speech-*")
	if err != nil {
		return cacheFailure()
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil || os.Rename(name, filepath.Join(cache.root, key+".json")) != nil {
		return cacheFailure()
	}
	if entry, ok := cache.entries[key]; ok {
		cache.bytes -= entry.size
	}
	cache.entries[key] = speechCacheEntry{size: int64(len(data)), used: time.Now()}
	cache.bytes += int64(len(data))
	return cache.prune()
}

func (cache *speechCache) prune() error {
	if cache.bytes <= speechCacheBytes && len(cache.entries) <= speechCacheFiles {
		return nil
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := cache.entries[keys[i]], cache.entries[keys[j]]
		if a.used.Equal(b.used) {
			return keys[i] < keys[j]
		}
		return a.used.Before(b.used)
	})
	for _, key := range keys {
		if cache.bytes <= speechCacheBytes && len(cache.entries) <= speechCacheFiles {
			break
		}
		err := os.Remove(filepath.Join(cache.root, key+".json"))
		if err != nil && !os.IsNotExist(err) {
			return cacheFailure()
		}
		cache.bytes -= cache.entries[key].size
		delete(cache.entries, key)
	}
	return nil
}
