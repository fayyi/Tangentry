package app

import (
	"tangentry/internal/config"
)

type services struct {
	// runtime owns persistence, documents and lifecycle-managed resources.
	runtime *runtimeResources
	// adapters is the external capability phase (platform clients and network).
	adapters *platforms
	// features contains long-lived domain services built on runtime/adapters.
	features *apiBundle
	// imports is a feature port assembled from the runtime and adapter phases.
	imports contentImporter
}

func newServices(dataDir string, configuration config.Config, option Options) (_ *services, started closers, err error) {
	defer func() {
		if err != nil {
			_ = started.close()
		}
	}()

	runtime, err := openRuntimeResources(dataDir, configuration, option, &started)
	if err != nil {
		return nil, started, err
	}
	features, err := wireFeatures(dataDir, configuration, option, runtime, &started)
	if err != nil {
		return nil, started, err
	}
	startBackgroundServices(features)
	return &services{
		runtime: runtime, adapters: runtime.platforms, features: features,
		imports: contentImporter{platforms: runtime.platforms.imports, records: runtime.repositories.xiaohongshu, documents: runtime.documents},
	}, started, nil
}
