package tools

import (
	"encoding/csv"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

const cloudUploadLimit = 25_000_000

type speechChunk struct {
	path       string
	start, end float64
}

// readSpeechChunks validates ffmpeg's manifest and the files it references
// before any provider receives audio.
func readSpeechChunks(manifest, root string) ([]speechChunk, error) {
	file, err := os.Open(manifest)
	if err != nil {
		return nil, storageFailure(err)
	}
	defer file.Close()
	reader := csv.NewReader(io.LimitReader(file, 128<<10))
	reader.FieldsPerRecord = 3
	rows, err := reader.ReadAll()
	if err != nil || len(rows) == 0 || len(rows) > 1441 {
		return nil, subtitles.ErrFormat
	}
	chunks := make([]speechChunk, 0, len(rows))
	var previous float64
	for index, row := range rows {
		name := "chunk-" + leftPadChunk(index) + ".mp3"
		if row[0] != name {
			return nil, subtitles.ErrFormat
		}
		start, e1 := strconv.ParseFloat(row[1], 64)
		end, e2 := strconv.ParseFloat(row[2], 64)
		if e1 != nil || e2 != nil || math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end <= start || (index == 0 && start > .1) || math.Abs(start-previous) > .1 {
			return nil, subtitles.ErrFormat
		}
		// MP3 padding can add a fraction of a second at the tail.
		if end > 21600.5 {
			return nil, transcription.Failed("audio_too_long", "云端转录暂支持六小时以内的媒体，请先截取较短片段。")
		}
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, subtitles.ErrFormat
		}
		if info.Size() > cloudUploadLimit {
			return nil, transcription.Failed("upload_too_large", "转录片段超过 25 MB，请使用本机转录。")
		}
		chunks = append(chunks, speechChunk{path: path, start: start, end: end})
		previous = end
	}
	return chunks, nil
}

func leftPadChunk(index int) string {
	value := strconv.Itoa(index)
	if len(value) >= 3 {
		return value
	}
	return strings.Repeat("0", 3-len(value)) + value
}
