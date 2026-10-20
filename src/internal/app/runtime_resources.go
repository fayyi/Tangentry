package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"tangentry/internal/adapters/filesystem"
	"tangentry/internal/adapters/sqlite"
	localstorage "tangentry/internal/adapters/storage/local"
	webdavstorage "tangentry/internal/adapters/storage/webdav"
	"tangentry/internal/adapters/tools"
	"tangentry/internal/backup"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/downloads"
)

type runtimeResources struct {
	store        *sqlite.Store
	preferences  *filesystem.Preferences
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
	preferencesStore := filesystem.NewPreferences(filepath.Join(dataDir, "preferences.json"))
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
	processing.SetDouyinResolver(p.discovery.douyin.Resolve)
	documents, err := filesystem.OpenDocuments(filepath.Join(dataDir, "documents"), p.network.public)
	if err != nil {
		return nil, err
	}
	records := repositories{xiaohongshu: store, subscription: store}
	backupWorkDir := filepath.Join(dataDir, ".tangentry-restore")
	backupService := backup.NewService(store, store, func(target backup.Target) (backup.ObjectStore, error) {
		switch target.Type {
		case "local":
			return localstorage.New(target.Path)
		case "webdav":
			password, ok := cookies.BackupTargetPassword(target.ID)
			if !ok {
				return nil, fmt.Errorf("WebDAV 凭据未配置")
			}
			return webdavstorage.New(webdavstorage.Config{Endpoint: target.Endpoint, Username: target.Username, Password: password, RemotePath: target.RemotePath, AllowInsecureTLS: target.AllowInsecureTLS, Timeout: 30 * time.Second, TempDir: backupWorkDir})
		default:
			return nil, fmt.Errorf("unsupported backup target type")
		}
	}, backupWorkDir, "dev")
	backupService.SetTargetCredentials(cookies)
	if err := backupService.CleanupOrphanTargetCredentials(context.Background()); err != nil {
		return nil, fmt.Errorf("backup credential cleanup: %w", err)
	}
	backupService.SetTargetTester(func(ctx context.Context, target backup.Target) error {
		store, err := webdavstorage.New(webdavstorage.Config{Endpoint: target.Endpoint, Username: target.Username, Password: target.Password, RemotePath: target.RemotePath, AllowInsecureTLS: target.AllowInsecureTLS, Timeout: 15 * time.Second, TempDir: backupWorkDir})
		if err != nil {
			return err
		}
		defer store.Close()
		return store.TestConnection(ctx)
	})
	return &runtimeResources{store: store, preferences: preferencesStore, cookies: cookies, toolkit: toolkit, platforms: p, documents: documents, downloads: processing, backup: backupService, repositories: records}, nil
}
