// Package desktop prepares the runtime packaged with the desktop app.
package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// runtimeTools are the packaged programs, by base name; Windows adds ".exe".
// Linux packages depend on the distribution's FFmpeg instead. yt-dlp, its
// JavaScript runtime and the Whisper model are not packaged: the app downloads
// verified copies into the data directory on first use.
func runtimeTools() []string {
	if runtime.GOOS == "linux" {
		return []string{"whisper-cli"}
	}
	return []string{"ffmpeg", "ffprobe", "whisper-cli"}
}

func toolPath(dir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, "bin", name)
}

// ConfigureRuntime uses stable paths even when the app bundle is moved or updated.
// Only packaged tools live behind this symlink.
func ConfigureRuntime(resources, dataDir string) error {
	runtimeDir := filepath.Join(resources, "runtime")
	for _, name := range runtimeTools() {
		filename := toolPath(runtimeDir, name)
		info, err := os.Stat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("packaged runtime is missing %s", filename)
		}
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	link, err := linkRuntime(runtimeDir, dataDir)
	if err != nil {
		return err
	}
	defaults := map[string]string{
		"TANGENTRY_WHISPER_PATH":       toolPath(link, "whisper-cli"),
		"TANGENTRY_WHISPER_MODELS_DIR": filepath.Join(dataDir, "models"),
		"TANGENTRY_TOOLS_DIR":          filepath.Join(dataDir, "tools"),
	}
	if runtime.GOOS != "linux" {
		defaults["TANGENTRY_FFMPEG_PATH"] = toolPath(link, "ffmpeg")
	}
	for key, value := range defaults {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	// Packaged tools and the libraries they load beside them come first.
	return os.Setenv("PATH", filepath.Join(link, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// linkRuntime points <data>/runtime at the packaged runtime and returns the
// path tools should be configured with. Where the system refuses symlinks
// (Windows without Developer Mode) the packaged path is used directly: Windows
// and Linux installers keep a fixed install location.
func linkRuntime(runtimeDir, dataDir string) (string, error) {
	link := filepath.Join(dataDir, "runtime")
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("runtime path is occupied by a non-symlink: %s", link)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.MkdirTemp(dataDir, ".runtime-link-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	staged := filepath.Join(temp, "runtime")
	if err := os.Symlink(runtimeDir, staged); err != nil {
		if runtime.GOOS == "windows" {
			return runtimeDir, nil
		}
		return "", err
	}
	// Windows cannot rename over an existing directory symlink, so a second
	// launch would fail here; drop the old link first (removing it leaves the
	// target untouched).
	if runtime.GOOS == "windows" {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return runtimeDir, nil
		}
	}
	if err := os.Rename(staged, link); err != nil {
		if runtime.GOOS == "windows" {
			return runtimeDir, nil
		}
		return "", err
	}
	return link, nil
}
