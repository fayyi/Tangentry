package main

import "testing"

func TestKeepRunningWithoutWindows(t *testing.T) {
	for _, test := range []struct {
		goos string
		want bool
	}{
		{goos: "darwin", want: true},
		{goos: "linux", want: false},
		{goos: "windows", want: false},
	} {
		if got := keepRunningWithoutWindows(test.goos); got != test.want {
			t.Fatalf("keepRunningWithoutWindows(%q) = %v, want %v", test.goos, got, test.want)
		}
	}
}
