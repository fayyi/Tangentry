package main

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"tangentry/internal/desktop"
)

func openDesktopLog() (*os.File, error) {
	logDir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(logDir, "desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

func configureDesktopRuntime() (string, error) {
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	resources, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return "", err
	}
	if err := desktop.ConfigureRuntime(resources, dataDir); err != nil {
		return "", err
	}
	return dataDir, nil
}
