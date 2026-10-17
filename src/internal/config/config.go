// Package config reads deployment settings once, outside business modules.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	BilibiliCookie string
	YTDLPPath      string
	FFmpegPath     string
	CookieFile     string
	DownloadDir    string
	DownloadLimit  time.Duration
	// ToolsDir holds tools downloaded on first use, such as yt-dlp. Empty
	// means tools come from PATH or explicit paths only.
	ToolsDir string
}

func Load(dataDir string) (Config, error) {
	c := Config{YTDLPPath: os.Getenv("TANGENTRY_YTDLP_PATH"), FFmpegPath: os.Getenv("TANGENTRY_FFMPEG_PATH"), CookieFile: os.Getenv("TANGENTRY_BILIBILI_COOKIE_FILE"), DownloadDir: os.Getenv("TANGENTRY_DOWNLOAD_DIR"), DownloadLimit: 2 * time.Hour}
	c.BilibiliCookie = os.Getenv("TANGENTRY_BILIBILI_COOKIE")
	c.ToolsDir = os.Getenv("TANGENTRY_TOOLS_DIR")
	if c.DownloadDir == "" {
		c.DownloadDir = filepath.Join(dataDir, "downloads")
	}
	var err error
	c.DownloadDir, err = filepath.Abs(c.DownloadDir)
	if err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("TANGENTRY_DOWNLOAD_TIMEOUT"); raw != "" {
		c.DownloadLimit, err = time.ParseDuration(raw)
		if err != nil || c.DownloadLimit < time.Second || c.DownloadLimit > 24*time.Hour {
			return Config{}, fmt.Errorf("TANGENTRY_DOWNLOAD_TIMEOUT must be between 1s and 24h")
		}
	}
	if c.CookieFile != "" {
		c.CookieFile, err = filepath.Abs(c.CookieFile)
		if err != nil {
			return Config{}, err
		}
		info, err := os.Stat(c.CookieFile)
		if err != nil || !info.Mode().IsRegular() {
			return Config{}, fmt.Errorf("TANGENTRY_BILIBILI_COOKIE_FILE must point to an existing Netscape cookie file")
		}
	}
	return c, nil
}
