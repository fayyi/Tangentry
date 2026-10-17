package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// macOSAdHocOrUnsigned reports whether the current executable lacks a stable
// signing identity. codesign writes its verbose inspection to stderr; this
// check only inspects the signature and never asks the keychain for access.
func macOSAdHocOrUnsigned() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return true
	}
	output, err := exec.Command("/usr/bin/codesign", "-dvvv", "--verbose=4", executable).CombinedOutput()
	if err != nil {
		return true
	}
	return !hasFormalMacOSSignature(string(output))
}

func hasFormalMacOSSignature(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "Signature=adhoc") {
			return false
		}
		if strings.HasPrefix(line, "Authority=") && strings.TrimPrefix(line, "Authority=") != "" {
			return true
		}
	}
	return false
}
