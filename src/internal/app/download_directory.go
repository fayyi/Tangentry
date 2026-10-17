package app

func (a *Application) DownloadDirectory() string { return a.shell.DownloadDirectory() }

func (a *Application) DownloadDirectorySelected() bool { return a.shell.DownloadDirectorySelected() }

func (a *Application) SetDownloadDirectory(directory string) error {
	return a.shell.SetDownloadDirectory(directory)
}
