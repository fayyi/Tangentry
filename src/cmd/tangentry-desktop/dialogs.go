package main

import (
	"context"

	"github.com/egoist/mygo"
)

func pickPath(ctx context.Context, kind, current string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	title := "选择文件"
	directory := kind == "download-directory" || kind == "model-directory" || kind == "backup-directory"
	switch kind {
	case "download-directory":
		title = "选择下载目录"
	case "whisper-binary":
		title = "选择 Whisper 程序"
	case "whisper-model":
		title = "选择 Whisper 模型"
	case "python":
		title = "选择 Python 程序"
	case "model-directory":
		title = "选择模型目录"
	case "backup-directory":
		title = "选择备份目录"
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.FocusedWindow(), Title: title, DefaultPath: current,
		Directory: directory, CreateDirectories: directory,
	})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}
