package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/egoist/mygo"
)

// desktopCapture exposes the native WebView compositor snapshot. Unlike a
// canvas read of a cross-origin iframe, CapturePage includes embedded frames.
type desktopCapture struct{}

func (desktopCapture) Capture(ctx context.Context) (string, error) {
	page := mygo.CallerPage(ctx)
	if page == nil || !strings.HasPrefix(page.URL(), "http://127.0.0.1:") {
		return "", fmt.Errorf("desktop capture is only available to the app page")
	}
	png, err := mygo.CallerWindow(ctx).CapturePage()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}
