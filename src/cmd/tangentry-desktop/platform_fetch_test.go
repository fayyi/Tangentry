package main

import (
	"strings"
	"testing"

	"tangentry/internal/app"
)

func TestValidatePlatformPath(t *testing.T) {
	for _, raw := range []string{"/latest.json", "/top.json?page=2", "/aweme/v2/web/module/feed/?refresh_index=1"} {
		if err := validatePlatformPath(raw); err != nil {
			t.Fatalf("valid path %q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "https://linux.do/latest.json", "//evil.example/latest.json", "/../latest.json", "/x/../latest.json", "/x\\evil", "/x\r\n"} {
		if err := validatePlatformPath(raw); err == nil {
			t.Fatalf("invalid path %q accepted", raw)
		}
	}
}

func TestPlatformPageOriginAndCloudflareRecognition(t *testing.T) {
	for _, test := range []struct {
		platform, origin string
	}{
		{"linuxdo", "https://linux.do/"},
		{"douyin", "https://www.douyin.com/"},
	} {
		origin, ok := platformPageOrigin(test.platform)
		if !ok || origin != test.origin {
			t.Fatalf("origin(%q) = %q, %v", test.platform, origin, ok)
		}
	}
	if _, ok := platformPageOrigin("other"); ok {
		t.Fatal("unsupported platform accepted")
	}
	for _, body := range []string{"<title>Just a moment...</title>", "cf-chl-abc", "challenge-platform", "Cloudflare"} {
		if !isCloudflarePage(httpStatusOK, body) {
			t.Fatalf("challenge body not recognized: %q", body)
		}
	}
	if !isCloudflarePage(403, "json") || isCloudflarePage(200, `{"ok":true}`) {
		t.Fatal("status/body challenge recognition is incorrect")
	}
}

func TestPlatformFetchScriptHonorsMethodBodyAndLimits(t *testing.T) {
	code, err := platformFetchScript("/feed.json", app.PlatformPageRequest{Method: "POST", Headers: map[string]string{"Accept": "application/json"}, Body: "a=1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"/feed.json", "method:\"POST\"", "body:\"a=1\"", "AbortController", "tooLarge"} {
		if !strings.Contains(code, part) {
			t.Fatalf("script does not contain %q: %s", part, code)
		}
	}
}

func TestPlatformFetchRequestAndResponseLimits(t *testing.T) {
	request := app.PlatformPageRequest{Method: "", Body: strings.Repeat("x", 256<<10)}
	if err := validatePlatformFetchRequest(&request); err != nil || request.Method != "GET" {
		t.Fatalf("default request rejected: %v, method=%q", err, request.Method)
	}
	request = app.PlatformPageRequest{Method: "PUT"}
	if err := validatePlatformFetchRequest(&request); err == nil {
		t.Fatal("unsupported method accepted")
	}
	request = app.PlatformPageRequest{Method: "POST", Body: strings.Repeat("x", 256<<10+1)}
	if err := validatePlatformFetchRequest(&request); err == nil {
		t.Fatal("oversized request accepted")
	}
	if err := validatePlatformFetchResponse(platformFetchResult{TooLarge: true}); err == nil {
		t.Fatal("oversized response marker accepted")
	}
	if err := validatePlatformFetchResponse(platformFetchResult{Body: strings.Repeat("x", platformFetchMaxBody+1)}); err == nil {
		t.Fatal("oversized response body accepted")
	}
	for _, request := range []app.PlatformPageRequest{
		{Method: "GET", Headers: map[string]string{"X-Test": "line\nfeed"}},
		{Method: "GET", Headers: map[string]string{"": "value"}},
		{Method: "GET", Headers: map[string]string{"X-Test": strings.Repeat("x", 8193)}},
	} {
		if err := validatePlatformFetchRequest(&request); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
}

func TestPlatformPageURLAllowed(t *testing.T) {
	for _, test := range []struct {
		platform, raw string
		allowed       bool
	}{
		{"linuxdo", "https://linux.do/", true},
		{"linuxdo", "https://www.linux.do/login", true},
		{"linuxdo", "https://challenges.cloudflare.com/", false},
		{"douyin", "https://www.douyin.com/", true},
		{"douyin", "https://passport.douyin.com/", false},
		{"douyin", "https://www.douyin.com.evil.example/", false},
	} {
		if got := platformPageURLAllowed(test.raw, test.platform); got != test.allowed {
			t.Fatalf("platformPageURLAllowed(%q, %q) = %v, want %v", test.raw, test.platform, got, test.allowed)
		}
	}
}

const httpStatusOK = 200
