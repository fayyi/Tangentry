package app

import (
	"fmt"
	"path/filepath"

	"tangentry/internal/adapters/filesystem"
	"tangentry/internal/adapters/sqlite"
	localstorage "tangentry/internal/adapters/storage/local"
	"tangentry/internal/adapters/tools"
	"tangentry/internal/backup"
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
	backup       *backup.Service
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
	toolkit.SetYouTubeUserAgent(func() string { return cookies.UserAgent("youtube") })
	processing, err := downloads.NewService(store, toolkit, configuration.DownloadDir, configuration.DownloadLimit)
	if err != nil {
		return nil, err
	}
	started.add(processing.Close)
	store.SetBackupDownloadDirectory(processing.Directory())
	p, err := newPlatforms(dataDir, configuration, toolkit, cookies, option)
	if err != nil {
		return nil, err
	}
	documents, err := filesystem.OpenDocuments(filepath.Join(dataDir, "documents"), p.network.public)
	if err != nil {
		return nil, err
	}
	records := repositories{xiaohongshu: store, subscription: store}
	// Keep restore plans and the pre-restore snapshot outside dataDir. The
	// application may atomically rename dataDir during confirmation; sibling
	// staging keeps the returned protection path valid for rollback afterwards.
	backupWorkDir := filepath.Join(filepath.Dir(dataDir), ".tangentry-backup-work")
	backupService := backup.NewService(store, store, func(target backup.Target) (backup.ObjectStore, error) {
		if target.Type != "local" {
			return nil, fmt.Errorf("unsupported backup target type")
		}
		return localstorage.New(target.Path)
	}, backupWorkDir, "dev")
	return &runtimeResources{store: store, cookies: cookies, toolkit: toolkit, platforms: p, documents: documents, downloads: processing, backup: backupService, repositories: records}, nil
}
