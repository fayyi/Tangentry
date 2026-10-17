package transcription

import (
	"encoding/hex"
	"strings"
)

func validConfig(c Config) bool {
	if len(c.MAICookie) > 16384 || strings.ContainsAny(c.MAICookie, "\r\n") {
		return false
	}
	for _, char := range c.MAICookie {
		if char < 32 || char > 126 {
			return false
		}
	}
	if c.SiliconflowModel != "" && c.SiliconflowModel != "FunAudioLLM/SenseVoiceSmall" && c.SiliconflowModel != "Qwen/Qwen3-ASR-1.7B" {
		return false
	}
	if len(c.Binary) > 2048 || len(c.Python) > 2048 || len(c.Model) > 2048 || len(c.QwenAligner) > 2048 || !validSecret(c.GroqAPIKey) || !validSecret(c.CloudflareAPIToken) || !validSecret(c.SiliconflowAPIKey) {
		return false
	}
	if c.SiliconflowSegmentSeconds != 0 && c.SiliconflowSegmentSeconds != 15 && c.SiliconflowSegmentSeconds != 30 && c.SiliconflowSegmentSeconds != 60 {
		return false
	}
	if c.GroqModel != "" && c.GroqModel != "whisper-large-v3" && c.GroqModel != "whisper-large-v3-turbo" {
		return false
	}
	if c.CloudflareAccountID != "" && !validAccountID(c.CloudflareAccountID) {
		return false
	}
	if !c.Cloud() {
		switch c.Backend {
		case "qwen3-asr", "faster-whisper":
			if c.Python == "" {
				return false
			}
		case "", "whisper.cpp":
			if c.Model == "" || c.Binary == "" {
				return false
			}
		default:
			return false
		}
	}
	return c.Language == "auto" || c.Language == "zh" || c.Language == "en" || c.Language == "ja" || c.Language == "ko"
}

// ValidateConfig is used by persistence adapters before accepting data from
// disk. Runtime updates continue to use the same rule through Configure.
func ValidateConfig(c Config) bool { return validConfig(c) }

func validSecret(value string) bool {
	if len(value) > 512 {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}

func validAccountID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
