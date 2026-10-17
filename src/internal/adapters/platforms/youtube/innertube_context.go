package youtube

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type contextData struct {
	hl, gl, remoteHost, visitorData, clientVersion, osName, osVersion, timeZone string
	browserName, browserVersion, deviceMake, deviceModel, appInstallData        string
	deviceExperimentID, rolloutToken                                            string
}

func buildContext(args contextData, now time.Time) map[string]any {
	_, offset := now.Zone()
	client := map[string]any{
		"hl": args.hl, "gl": args.gl, "remoteHost": args.remoteHost,
		"screenDensityFloat": 1, "screenHeightPoints": 1440, "screenPixelDensity": 1, "screenWidthPoints": 2560,
		"visitorData": args.visitorData, "clientName": "WEB", "clientVersion": args.clientVersion,
		"osName": args.osName, "osVersion": args.osVersion, "userAgent": innertubeUserAgent, "platform": "DESKTOP",
		"clientFormFactor": "UNKNOWN_FORM_FACTOR", "userInterfaceTheme": "USER_INTERFACE_THEME_LIGHT",
		"timeZone": args.timeZone, "originalUrl": innertubeBase, "deviceMake": args.deviceMake, "deviceModel": args.deviceModel,
		"browserName": args.browserName, "browserVersion": args.browserVersion, "utcOffsetMinutes": offset / 60,
		"memoryTotalKbytes": "8000000",
		"mainAppWebInfo": map[string]any{
			"graftUrl": innertubeBase, "pwaInstallabilityStatus": "PWA_INSTALLABILITY_STATUS_UNKNOWN",
			"webDisplayMode": "WEB_DISPLAY_MODE_BROWSER", "isWebNativeShareAvailable": true,
		},
	}
	if args.rolloutToken != "" {
		client["rolloutToken"] = args.rolloutToken
	}
	if args.deviceExperimentID != "" {
		client["deviceExperimentId"] = args.deviceExperimentID
	}
	if args.appInstallData != "" {
		client["configInfo"] = map[string]any{"appInstallData": args.appInstallData}
	}
	return map[string]any{
		"client":  client,
		"user":    map[string]any{"enableSafetyMode": false, "lockedSafetyMode": false},
		"request": map[string]any{"useSsl": true, "internalExperimentFlags": []any{}},
	}
}

const randomAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func randomString(length int) string {
	buffer := make([]byte, length)
	_, _ = rand.Read(buffer)
	for i, value := range buffer {
		buffer[i] = randomAlphabet[int(value)%len(randomAlphabet)]
	}
	return string(buffer)
}

// localVisitorData encodes the VisitorData protobuf {1: id, 5: timestamp}.
func localVisitorData(now time.Time) string {
	id := randomString(11)
	message := []byte{0x0a, byte(len(id))}
	message = append(message, id...)
	message = append(message, 0x28)
	message = binary.AppendUvarint(message, uint64(uint32(now.Unix())))
	encoded := base64.StdEncoding.EncodeToString(message)
	encoded = strings.NewReplacer("+", "-", "/", "_").Replace(encoded)
	return url.QueryEscape(encoded)
}

func localTimeZone() string {
	if zone := os.Getenv("TZ"); zone != "" && !strings.HasPrefix(zone, ":") {
		return zone
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(filepath.ToSlash(target), "zoneinfo/"); ok {
			return zone
		}
	}
	return "UTC"
}

// path walks decoded JSON objects by key.
func path(value any, keys ...string) any {
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func index(value any, i int) any {
	items, ok := value.([]any)
	if !ok || i < 0 || i >= len(items) {
		return nil
	}
	return items[i]
}

func stringAt(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
