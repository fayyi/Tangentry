package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func packagedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var files []string
	for _, name := range runtimeTools() {
		files = append(files, toolPath(filepath.Join(root, "runtime"), name))
	}
	for _, filename := range files {
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte("fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRuntimePathsSurviveMovingTheBundle(t *testing.T) {
	for _, name := range []string{
		"PATH", "TANGENTRY_FFMPEG_PATH", "TANGENTRY_YTDLP_PATH", "TANGENTRY_WHISPER_PATH",
		"TANGENTRY_WHISPER_SERVER_PATH", "TANGENTRY_WHISPER_MODEL", "TANGENTRY_WHISPER_MODELS_DIR", "TANGENTRY_TOOLS_DIR",
	} {
		t.Setenv(name, os.Getenv(name))
	}
	os.Unsetenv("TANGENTRY_YTDLP_PATH")
	os.Unsetenv("TANGENTRY_WHISPER_MODEL")
	first, second := packagedFixture(t), packagedFixture(t)
	data := t.TempDir()
	if err := ConfigureRuntime(first, data); err != nil {
		t.Fatal(err)
	}
	whisper := os.Getenv("TANGENTRY_WHISPER_PATH")
	if err := ConfigureRuntime(second, data); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TANGENTRY_WHISPER_PATH") != whisper {
		t.Fatal("persisted tool path changed when the bundle moved")
	}
	resolved, err := filepath.EvalSymlinks(whisper)
	expected, expectedErr := filepath.EvalSymlinks(toolPath(filepath.Join(second, "runtime"), "whisper-cli"))
	if err != nil || expectedErr != nil || resolved != expected {
		t.Fatalf("tool did not follow the new bundle: %s, %v", resolved, err)
	}
	// Downloads on first use live in the data directory, never in the bundle.
	if os.Getenv("TANGENTRY_TOOLS_DIR") != filepath.Join(data, "tools") || os.Getenv("TANGENTRY_WHISPER_MODELS_DIR") != filepath.Join(data, "models") {
		t.Fatal("first-use downloads are not kept in the data directory")
	}
	if os.Getenv("TANGENTRY_YTDLP_PATH") != "" || os.Getenv("TANGENTRY_WHISPER_MODEL") != "" {
		t.Fatal("the runtime still points at packaged yt-dlp or models")
	}
	if err := os.Remove(toolPath(filepath.Join(second, "runtime"), "whisper-cli")); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRuntime(second, data); err == nil {
		t.Fatal("incomplete bundle accepted")
	}
}

func TestRuntimeDoesNotOverwriteUserDirectory(t *testing.T) {
	data := t.TempDir()
	if err := os.Mkdir(filepath.Join(data, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRuntime(packagedFixture(t), data); err == nil {
		t.Fatal("occupied runtime directory accepted")
	}
}
