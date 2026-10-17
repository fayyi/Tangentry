package app

import (
	"path/filepath"

	"tangentry/internal/adapters/filesystem"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/adapters/tools"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/downloads"
)

type runtimeResources struct {
	store        *sqlite.Store
	cookies      *credentials.Store
	toolkit      *tools.Toolkit
	platforms    *platforms
	documents    *filesystem.Documents
	downloads    *downloads.Service
	repositories repositories
}

// openRuntimeResources owns low-level resources and external clients. Its
// caller owns the returned closers and closes them if a later wiring step fails.
func openRuntimeResources(dataDir string, configuration config.Config, option Options, started *closers) (*runtimeResources, error) {
	store, err := sqlite.Open(filepath.Join(dataDir, "tangentry.db"))
	if err != nil {
		return nil, err
	}
	started.addErr(store.Close)
	credentialDir := filepath.Join(dataDir, "credentials")
	var cookies *credentials.Store
	if len(option.CredentialKey) > 0 {
		cookies, err = credentials.OpenWithKey(credentialDir, configuration.BilibiliCookie, configuration.CookieFile, option.CredentialKey)
	} else {
		cookies, err = credentials.Open(credentialDir, configuration.BilibiliCookie, configuration.CookieFile)
	}
	if err != nil {
		return nil, err
	}
	toolkit := tools.New(configuration)
	toolkit.SetCredentials(cookies)
	processing, err := downloads.NewService(store, toolkit, configuration.DownloadDir, configuration.DownloadLimit)
	if err != nil {
		return nil, err
	}
	started.add(processing.Close)
	p, err := newPlatforms(dataDir, configuration, toolkit, cookies, option)
	if err != nil {
		return nil, err
	}
	documents, err := filesystem.OpenDocuments(filepath.Join(dataDir, "documents"), p.network.public)
	if err != nil {
		return nil, err
	}
	records := repositories{xiaohongshu: store, subscription: store}
	return &runtimeResources{store: store, cookies: cookies, toolkit: toolkit, platforms: p, documents: documents, downloads: processing, repositories: records}, nil
}
