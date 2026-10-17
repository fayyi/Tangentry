package tools

import "tangentry/internal/transcription"

func (t *Transcriber) pendingModel(c transcription.Config) (transcription.ModelOption, bool) {
	if c.Backend != "whisper.cpp" || regularFile(c.Model) || t.Models == nil {
		return transcription.ModelOption{}, false
	}
	return t.Models.catalogGGML(c.Model)
}

func (t *Transcriber) Status(c transcription.Config) transcription.Status {
	missing := []string{}
	if executable(t.FFmpeg, "ffmpeg") == "" {
		missing = append(missing, "FFmpeg")
	}
	if c.Cloud() {
		if c.Backend == "mai" && c.MAICookie == "" {
			missing = append(missing, "Microsoft Playground 登录 Cookie")
		}
		if c.Backend == "siliconflow" && c.SiliconflowAPIKey == "" {
			missing = append(missing, "硅基流动 API Key")
		}
		if c.Backend == "groq" && c.GroqAPIKey == "" {
			missing = append(missing, "Groq API Key")
		}
		if c.Backend == "cloudflare" {
			if c.CloudflareAPIToken == "" {
				missing = append(missing, "Cloudflare API Token")
			}
			if c.CloudflareAccountID == "" {
				missing = append(missing, "Cloudflare Account ID")
			}
		}
	} else if c.Backend == "qwen3-asr" {
		if !qwenInstalled(c.Python) {
			missing = append(missing, "qwen-asr Python 环境（含 PyTorch）")
		}
		if !qwenModel(c.Model, false) {
			missing = append(missing, "Qwen3-ASR 本地模型目录")
		}
		if !qwenModel(c.QwenAligner, true) {
			missing = append(missing, "Qwen3-ForcedAligner 本地模型目录")
		}
	} else if c.Backend == "faster-whisper" {
		if !fasterInstalled(c.Python) {
			missing = append(missing, "faster-whisper Python 环境")
		}
		if !fasterModel(c.Model) {
			missing = append(missing, "CTranslate2 模型目录（model.bin / config.json / tokenizer.json）")
		}
	} else if executable(c.Binary, "whisper-cli") == "" {
		missing = append(missing, "whisper.cpp")
	}
	status := transcription.Status{Config: c, Models: t.models(c)}
	if option, ok := t.pendingModel(c); ok {
		status.ModelDownload = option.Size
	} else if c.Backend == "whisper.cpp" && !regularFile(c.Model) {
		missing = append(missing, "Whisper 模型文件")
	}
	status.Ready, status.Missing = len(missing) == 0, missing
	return status
}
