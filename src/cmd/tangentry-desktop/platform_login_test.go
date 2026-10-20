package main

import (
	"strings"
	"testing"
)

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
		{"douyin", "https://aweme.snssdk.com/aweme/v1/play/?video_id=fixture", true},
		{"douyin", "https://passport.douyin.com/login", true},
		{"douyin", "https://www.iesdouyin.com/share", true},
		{"douyin", "https://douyin.com.evil.example/", false},
		{"douyin", "https://www.douyin.com:443/", false},
		{"bilibili", "https://passport.bilibili.com/login", true},
		{"bilibili", "https://evil.bilibili.com.evil.example/", false},
		{"xiaohongshu", "https://www.xiaohongshu.com/explore", true},
		{"xiaohongshu", "https://edith.xiaohongshu.com/api", true},
		{"youtube", "https://accounts.google.com/signin", true},
		{"youtube", "https://www.youtube.com/", true},
		{"youtube", "https://mail.google.com/", false},
	}
	for _, test := range tests {
		t.Run(test.platform+"/"+test.url, func(t *testing.T) {
			if got := platformLoginURLAllowed(test.url, test.platform); got != test.allowed {
				t.Fatalf("platformLoginURLAllowed(%q, %q) = %v, want %v", test.url, test.platform, got, test.allowed)
			}
		})
	}
}

func TestPlatformLoginOriginsAndCookieDomains(t *testing.T) {
	for _, platform := range []string{"bilibili", "linuxdo", "douyin", "xiaohongshu", "youtube"} {
		if origin, ok := platformLoginOrigin(platform); !ok || !strings.HasPrefix(origin, "https://") {
			t.Fatalf("origin unavailable for %s: %q", platform, origin)
		}
		if len(platformLoginCookieDomains(platform)) == 0 {
			t.Fatalf("cookie domains unavailable for %s", platform)
		}
	}
}
