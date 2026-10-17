package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCredentialKeyCreatesPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	key, err := fileCredentialKey(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	path := filepath.Join(dataDir, "credentials", "desktop.key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("key permissions = %o, want 600", got)
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := directory.Mode().Perm(); got != 0700 {
		t.Fatalf("directory permissions = %o, want 700", got)
	}
	again, err := fileCredentialKey(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, again) {
		t.Fatal("reading the local key changed it")
	}
	other := bytes.Repeat([]byte{0x44}, 32)
	if err := writeCredentialKey(dataDir, other); err != nil {
		t.Fatal(err)
	}
	still, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(still, key) {
		t.Fatal("signed key migration overwrote an existing local key")
	}
}

func TestFormalMacOSSignatureDetection(t *testing.T) {
	if hasFormalMacOSSignature("Signature=adhoc\n") {
		t.Fatal("ad-hoc signature considered formal")
	}
	if !hasFormalMacOSSignature("Authority=Developer ID Application: Tangentry\n") {
		t.Fatal("Developer ID signature not recognized")
	}
	if hasFormalMacOSSignature("code object is not signed at all\n") {
		t.Fatal("unsigned executable considered formal")
	}
}

func TestCredentialKeySourceSelection(t *testing.T) {
	for _, test := range []struct {
		goos  string
		adHoc bool
		want  bool
	}{
		{goos: "darwin", adHoc: true, want: false},
		{goos: "darwin", adHoc: false, want: true},
		{goos: "linux", adHoc: true, want: true},
		{goos: "windows", adHoc: true, want: true},
	} {
		if got := useSystemCredentialStore(test.goos, test.adHoc); got != test.want {
			t.Fatalf("useSystemCredentialStore(%q, %v) = %v, want %v", test.goos, test.adHoc, got, test.want)
		}
	}
}
