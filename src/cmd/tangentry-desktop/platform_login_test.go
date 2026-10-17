package main

import "testing"

func TestPlatformLoginURLAllowlist(t *testing.T) {
	tests := []struct {
		platform string
		url      string
		allowed  bool
	}{
		{"linuxdo", "https://linux.do/login", true},
		{"linuxdo", "https://www.linux.do/session", true},
		{"linuxdo", "https://challenges.cloudflare.com/turnstile", true},
		{"linuxdo", "https://evil.example/", false},
		{"linuxdo", "http://linux.do/", false},
		{"linuxdo", "https://linux.do.evil.example/", false},
		{"douyin", "https://www.douyin.com/", true},
		{"douyin", "https://passport.douyin.com/login", true},
		{"douyin", "https://www.iesdouyin.com/share", true},
		{"douyin", "https://douyin.com.evil.example/", false},
		{"douyin", "https://www.douyin.com:443/", false},
	}
	for _, test := range tests {
		t.Run(test.platform+"/"+test.url, func(t *testing.T) {
			if got := platformLoginURLAllowed(test.url, test.platform); got != test.allowed {
				t.Fatalf("platformLoginURLAllowed(%q, %q) = %v, want %v", test.url, test.platform, got, test.allowed)
			}
		})
	}
}
