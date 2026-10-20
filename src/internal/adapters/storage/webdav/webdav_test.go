package webdav

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tangentry/internal/backup"
)

func davResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/root/tangentry/v1/snapshots/fixture.tgb</d:href></d:response></d:multistatus>`)
}

func TestStoreBasicAuthPrefixAndAtomicUpload(t *testing.T) {
	objects := map[string][]byte{}
	var objectMu sync.Mutex
	var moves atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || pass != "fixture-password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/dav/root") {
			t.Errorf("path prefix missing: %s", r.URL.Path)
		}
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			objectMu.Lock()
			objects[r.URL.Path] = data
			objectMu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "MOVE":
			destination, err := url.Parse(r.Header.Get("Destination"))
			if err != nil {
				t.Error(err)
				return
			}
			objectMu.Lock()
			objects[destination.Path] = objects[r.URL.Path]
			delete(objects, r.URL.Path)
			objectMu.Unlock()
			moves.Add(1)
			w.WriteHeader(http.StatusCreated)
		case "DELETE":
			objectMu.Lock()
			delete(objects, r.URL.Path)
			objectMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case "PROPFIND":
			davResponse(w)
		case http.MethodGet:
			objectMu.Lock()
			data := append([]byte(nil), objects[r.URL.Path]...)
			objectMu.Unlock()
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store, err := New(Config{Endpoint: server.URL + "/dav", Username: "fixture-user", Password: "fixture-password", RemotePath: "root", TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Upload(context.Background(), "tangentry/v1/snapshots/fixture.tgb", strings.NewReader("encrypted-fixture")); err != nil {
		t.Fatal(err)
	}
	objectMu.Lock()
	finalObject := append([]byte(nil), objects["/dav/root/tangentry/v1/snapshots/fixture.tgb"]...)
	objectMu.Unlock()
	if moves.Load() != 1 || string(finalObject) != "encrypted-fixture" {
		t.Fatalf("MOVE did not finalize upload: moves=%d object=%q", moves.Load(), finalObject)
	}
	r, err := store.Read(context.Background(), "tangentry/v1/snapshots/fixture.tgb")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(data) != "encrypted-fixture" {
		t.Fatalf("read %q: %v", data, err)
	}
	items, err := store.List(context.Background(), "tangentry/v1/snapshots")
	if err != nil || len(items) != 1 {
		t.Fatalf("list %v: %v", items, err)
	}
}

func TestStoreUnauthorizedAndCrossHostRedirect(t *testing.T) {
	var redirected atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(r.Header.Get("Authorization") != "")
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	store, err := New(Config{Endpoint: redirect.URL, Username: "fixture-user", Password: "fixture-password"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect was not refused: %v", err)
	}
	if redirected.Load() {
		t.Fatal("Basic credentials reached cross-host redirect")
	}
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer unauthorized.Close()
	unauthorizedStore, _ := New(Config{Endpoint: unauthorized.URL, Username: "fixture-user", Password: "fixture-password"})
	defer unauthorizedStore.Close()
	if err := unauthorizedStore.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 not returned: %v", err)
	}
}

func TestStoreTimeoutCancellationAndUploadCleanup(t *testing.T) {
	var temporaryName string
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			temporaryName = r.URL.Path
			puts.Add(1)
			_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4))
			http.Error(w, "interrupted", http.StatusInternalServerError)
		case http.MethodDelete:
			if r.URL.Path != temporaryName {
				t.Errorf("cleanup path %q != temp %q", r.URL.Path, temporaryName)
			}
			w.WriteHeader(http.StatusNoContent)
		case "PROPFIND":
			time.Sleep(150 * time.Millisecond)
			davResponse(w)
		}
	}))
	defer server.Close()
	store, err := New(Config{Endpoint: server.URL, Timeout: 50 * time.Millisecond, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TestConnection(context.Background()); err == nil {
		t.Fatal("timeout was not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Upload(ctx, "one.tgb", strings.NewReader("fixture")); err == nil {
		t.Fatal("cancelled upload proceeded")
	}
	if puts.Load() != 0 {
		t.Logf("fixture observed %d interrupted upload attempts", puts.Load())
	}
}

func TestStoreInterruptRemovesTemporaryObjectAndNeverFinalizes(t *testing.T) {
	objects := map[string]bool{}
	var moveCount atomic.Int32
	var tempPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			tempPath = r.URL.Path
			_, _ = io.Copy(io.Discard, r.Body)
			objects[tempPath] = true
			http.Error(w, "interrupted", http.StatusInternalServerError)
		case "MOVE":
			moveCount.Add(1)
			w.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	store, _ := New(Config{Endpoint: server.URL, TempDir: t.TempDir()})
	defer store.Close()
	err := store.Upload(context.Background(), "tangentry/v1/snapshots/x.tgb", strings.NewReader("encrypted"))
	if err == nil || objects[tempPath] || moveCount.Load() != 0 {
		t.Fatalf("upload interruption leaked/finalized object: err=%v temp=%v moves=%d", err, objects[tempPath], moveCount.Load())
	}
}

func TestStorePathTraversalAndOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Length", fmt.Sprint(backup.MaxPackageSize+1))
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write(make([]byte, maxPropfind+1))
	}))
	defer server.Close()
	store, _ := New(Config{Endpoint: server.URL})
	defer store.Close()
	if _, err := store.Read(context.Background(), "../escape"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := New(Config{Endpoint: server.URL + "/dav/%2e%2e/private"}); err == nil {
		t.Fatal("encoded endpoint path traversal accepted")
	}
	if _, err := New(Config{Endpoint: server.URL + "/dav/%252e%252e/private"}); err == nil {
		t.Fatal("double-encoded endpoint path traversal accepted")
	}
	if _, err := store.List(context.Background(), ""); err == nil {
		t.Fatal("oversized PROPFIND accepted")
	}
}

func TestStoreDoesNotRetryPermanentHTTPFailures(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	store, err := New(Config{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TestConnection(context.Background()); err == nil {
		t.Fatal("permanent failure was accepted")
	}
	if requests.Load() != 1 {
		t.Fatalf("permanent HTTP failure retried %d times", requests.Load())
	}
}

func TestStoreConnectionTestRejectsOversizedUnknownLengthResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write(make([]byte, maxPropfind+1))
	}))
	defer server.Close()
	store, err := New(Config{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TestConnection(context.Background()); !errors.Is(err, backup.ErrInvalidPackage) {
		t.Fatalf("oversized connection response was accepted: %v", err)
	}
}

func TestStoreReadRejectsOversizedUnknownLengthWhileStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "0123456789abcdef")
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()
	store, err := New(Config{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.readLimit = 8
	response, err := store.Read(context.Background(), "snapshot.tgb")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Close()
	if _, err := io.Copy(io.Discard, response); !errors.Is(err, backup.ErrInvalidPackage) {
		t.Fatalf("unknown length body was not hard limited: %v", err)
	}
}

func TestSelfSignedTLSRequiresExplicitOption(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { davResponse(w) }))
	defer server.Close()
	secure, _ := New(Config{Endpoint: server.URL})
	defer secure.Close()
	if err := secure.TestConnection(context.Background()); err == nil {
		t.Fatal("self-signed certificate accepted by default")
	}
	insecure, _ := New(Config{Endpoint: server.URL, AllowInsecureTLS: true})
	defer insecure.Close()
	if err := insecure.TestConnection(context.Background()); err != nil {
		t.Fatalf("explicit self-signed option failed: %v", err)
	}
	if tr := insecure.client.Transport.(*http.Transport); tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS minimum version changed")
	}
}
