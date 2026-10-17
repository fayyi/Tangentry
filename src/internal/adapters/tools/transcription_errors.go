package tools

import (
	"errors"
	"strings"
	"syscall"

	"tangentry/internal/transcription"
)

func storageFailure(err error) error {
	if errors.Is(err, syscall.ENOSPC) {
		return transcription.Failed("disk_full", "转写磁盘空间不足，请清理临时目录所在磁盘后重试。")
	}
	return transcription.Failed("temporary_storage", "无法写入转写临时文件，请检查临时目录权限与磁盘空间。")
}

// toolFailure translates process output into stable product-level error codes.
func toolFailure(stage, output string) error {
	text := strings.ToLower(output)
	switch {
	case strings.Contains(text, "no space left"):
		return transcription.Failed("disk_full", "转写磁盘空间不足，请清理临时目录所在磁盘后重试。")
	case stage == "converting":
		return transcription.Failed("audio_conversion", "FFmpeg 无法解码音轨，媒体可能不完整或没有可识别的音频。")
	case strings.Contains(text, "out of memory"), strings.Contains(text, "bad_alloc"), strings.Contains(text, "cannot allocate memory"):
		return transcription.Failed("model_memory", "识别模型所需内存不足，请选择较小模型后重试。")
	case strings.Contains(text, "no module named"):
		return transcription.Failed("python_dependencies", "转录 Python 环境缺少依赖，请检查所选本机引擎的 Python 环境。")
	case strings.Contains(text, "local_files_only"), strings.Contains(text, "offline mode"), strings.Contains(text, "not a local folder"):
		return transcription.Failed("local_model_missing", "Qwen 模型目录不完整或不是本地模型；请检查模型文件，不会自动联网下载。")
	case strings.Contains(text, "qwen") && (strings.Contains(text, "out of memory") || strings.Contains(text, "bad_alloc")):
		return transcription.Failed("model_memory", "Qwen3-ASR 需要较多内存；可改用 0.6B 模型或较短音频。")
	default:
		return transcription.Failed("recognition_failed", "本机语音识别失败，请检查模型文件与转录引擎是否匹配。")
	}
}
