package xiaoyuzhou

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transcriptTransport func(*http.Request) (*http.Response, error)

func (f transcriptTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfficialTranscriptUsesNativeMediaAndIsolatesToken(t *testing.T) {
	id := "6accb466000000001802b69b"
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: transcriptTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch calls {
		case 1:
			if r.Header.Get("x-jike-access-token") != "synthetic-token" || r.URL.Query().Get("eid") != id {
				t.Fatal("missing API identity or token")
			}
			if r.Header.Get("Market") != "AppStore" || r.Header.Get("BundleID") != "app.podcast.cosmos" || r.Header.Get("x-jike-device-id") == "" {
				t.Fatal("missing native app headers")
			}
			body = `{"data":{"eid":"` + id + `","duration":100,"transcriptMediaId":"native-media","media":{"id":"external-audio"}}}`
		case 2:
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"mediaId":"native-media"`) {
				t.Fatal("external media selected")
			}
			body = `{"data":{"data":{"url":"https://media.xyzcdn.net/transcript.json"}}}`
		case 3:
			if r.Header.Get("x-jike-access-token") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("User-Agent") != appAgent {
				t.Fatal("unsafe CDN headers")
			}
			body = `[{"startMs":1250,"text":"第一段"},{"startMs":3000,"text":"第二段"}]`
		default:
			t.Fatal("unexpected request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	doc, err := c.Transcript(context.Background(), id, "synthetic-token")
	if err != nil || len(doc.Tracks) != 1 || doc.Tracks[0].Cues[0].Start != 1.25 || doc.Tracks[0].Cues[0].End != 3 || doc.Tracks[0].Cues[1].End != 100 {
		t.Fatalf("invalid transcript: %+v %v", doc, err)
	}
	_, err = c.Transcript(context.Background(), id, "")
	if err != nil || calls != 3 {
		t.Fatal("anonymous access requested private transcript")
	}
}

func TestOfficialTranscriptRejectsInvalidTiming(t *testing.T) {
	for _, raw := range []string{`[]`, `[{"text":"missing"}]`, `[{"startMs":-1,"text":"bad"}]`, `[{"startMs":0,"text":"one"},{"startMs":0,"text":"two"}]`, `[{"startMs":100000,"text":"past end"}]`} {
		if _, err := parseTranscript([]byte(raw), 100); err == nil {
			t.Fatal("invalid transcript accepted", raw)
		}
	}
}
