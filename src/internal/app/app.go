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
	"tangentry/internal/config"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
	"tangentry/internal/favoritesync"
	"tangentry/internal/playback"
	"tangentry/internal/transcription"
)

type Application struct {
	runtime       *runtimeState
	shell         *shellState
	subscriptions subscriptionManager
}

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
	services, started, err := newServices(dataDir, configuration, option)
	if err != nil {
		return nil, err
	}
	runtime := &runtimeState{
		handler:       newHTTPHandler(newHTTPWiring(services), assets),
		downloads:     services.features.downloads,
		transcription: services.features.transcription,
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
	return &Application{
		runtime:       runtime,
		shell:         shell,
		subscriptions: services.features.sources,
	}, nil
}

type runtimeState struct {
	handler       http.Handler
	downloads     *downloads.Service
	transcription *transcription.Service
	closers       closers
	closeOnce     sync.Once
	closeErr      error
}

func (r *runtimeState) HTTPHandler() http.Handler { return r.handler }

func (r *runtimeState) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.closers.close() })
	return r.closeErr
}

func (a *Application) HTTPHandler() http.Handler { return a.runtime.HTTPHandler() }

func (a *Application) Close() error { return a.runtime.Close() }
