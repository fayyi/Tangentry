package app

import (
	"context"
	"tangentry/internal/config"
	"testing"

	"tangentry/internal/platformfetch"
)

type appPageFetcher struct{}

func (appPageFetcher) Fetch(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
	return platformfetch.Response{}, nil
}

func TestPlatformPageFetcherIsInjectedIntoLinuxdoAndDouyin(t *testing.T) {
	fetcher := appPageFetcher{}
	dataDir := t.TempDir()
	services, started, err := newServices(dataDir, config.Config{DownloadDir: dataDir}, Options{PlatformPageFetcher: fetcher})
	if err != nil {
		t.Fatal(err)
	}
	defer started.close()
	if services.adapters.discovery.forum.Page != fetcher {
		t.Fatal("Linux.do client did not receive page fetcher")
	}
	if services.adapters.discovery.douyin.Page != fetcher {
		t.Fatal("Douyin client did not receive page fetcher")
	}
}
