// Package app is the composition root; feature modules do not import it.
package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"sync"

	"tangentry/internal/ai"
	"tangentry/internal/backup"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
	"tangentry/internal/favoritesync"
	"tangentry/internal/platformfetch"
	"tangentry/internal/playback"
	"tangentry/internal/transcription"
)

type Application struct {
	runtime       *runtimeState
	shell         *shellState
	subscriptions subscriptionManager
	dataDir       string
	assets        fs.FS
	option        Options
	mu            sync.Mutex
}

// PlatformPageFetcher is the native-shell port for same-origin requests in a
// platform WebView. The underlying interface lives in the dependency-neutral
// platformfetch package so adapters can consume it without importing app.
type PlatformPageFetcher = platformfetch.Fetcher
type PlatformPageRequest = platformfetch.Request
type PlatformPageResponse = platformfetch.Response

// Options replaces external platform I/O. Test fixtures are outside product code.
type Options struct {
	AITranslator        ai.Translator
	AISummarizer        ai.Summarizer
	FavoritesRemote     favoritesync.Remote
	TranscriptionEngine transcription.Engine
	Playback            func(context.Context, string) (playback.Source, error)
	BilibiliAudio       func(context.Context, string) (transcription.Audio, error)
	Recommendations     func(context.Context, string) (discovery.Feed, error)
	Platform            discovery.Provider
	PublicHTTP          *http.Client
	// PlatformPageFetcher lets a native shell execute same-origin requests in
	// the platform's authenticated WebView. A nil value keeps headless/server
	// builds on their existing HTTP clients.
	PlatformPageFetcher platformfetch.Fetcher
	// CredentialKey is supplied by a native shell from its system credential
	// store. A nil key keeps headless/server storage compatible with older data.
	CredentialKey []byte
}

// closers releases services in reverse start order.
type closers []func() error

func (c *closers) add(close func()) {
	*c = append(*c, func() error { close(); return nil })
}

func (c *closers) addErr(close func() error) { *c = append(*c, close) }

func (c closers) close() error {
	var err error
	for i := len(c) - 1; i >= 0; i-- {
		err = errors.Join(err, c[i]())
	}
	return err
}

func New(dataDir string, assets fs.FS, options ...Options) (application *Application, err error) {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	configuration, err := config.Load(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := backup.ApplyPendingRestore(dataDir); err != nil {
		return nil, err
	}
	services, started, err := newServices(dataDir, configuration, option)
	if err != nil {
		return nil, err
	}
	runtime := &runtimeState{
		handler:       newHTTPHandler(newHTTPWiring(services), assets),
		downloads:     services.features.downloads,
		transcription: services.features.transcription,
		cookies:       services.runtime.cookies,
		closers:       started,
	}
	shell := &shellState{
		downloads: services.features.downloads,
		models: &modelDirectoryState{
			modelDownloads: services.features.modelDownloads,
			transcription:  services.features.transcription,
			modelDir:       services.features.modelDir,
		},
	}
	application = &Application{
		runtime:       runtime,
		shell:         shell,
		subscriptions: services.features.sources,
		dataDir:       dataDir, assets: assets, option: option,
	}
	return application, nil
}

type runtimeState struct {
	handler       http.Handler
	downloads     *downloads.Service
	transcription *transcription.Service
	cookies       *credentials.Store
	closers       closers
	closeOnce     sync.Once
	closeErr      error
}

func (r *runtimeState) HTTPHandler() http.Handler { return r.handler }

func (r *runtimeState) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.closers.close() })
	return r.closeErr
}

func (a *Application) HTTPHandler() http.Handler {
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	return runtime.HTTPHandler()
}

func (a *Application) Close() error { return a.runtime.Close() }
