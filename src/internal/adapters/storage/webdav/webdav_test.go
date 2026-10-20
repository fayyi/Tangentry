package webdav

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

func folderResponseXML(entries ...string) string {
	return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">` + strings.Join(entries, "") + `</d:multistatus>`
}

func folderResponse(href, resourceType string) string {
	return `<d:response><d:href>` + href + `</d:href><d:propstat><d:prop><d:resourcetype>` + resourceType + `</d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`
}

func newIPv4DAVServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTestServer(t, handler)
	_ = server.Client()
	t.Cleanup(server.Close)
	return server
}

func newIPv4DAVTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTestServer(t, handler)
	_ = server.Client()
	server.URL = strings.Replace(server.URL, "http://", "https://", 1)
	return server
}

func newTestStore(t *testing.T, server *httptest.Server, cfg Config) *Store {
	t.Helper()
	store, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport.(*http.Transport).Clone()
	if transport.TLSClientConfig != nil {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		if !cfg.AllowInsecureTLS {
			transport.TLSClientConfig.InsecureSkipVerify = true
			transport.TLSClientConfig.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error {
				return errors.New("fixture certificate is not trusted")
			}
		} else {
			transport.TLSClientConfig.InsecureSkipVerify = true
			transport.TLSClientConfig.VerifyPeerCertificate = nil
		}
	}
	store.client.Transport = transport
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func davResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/root/tangentry/v1/snapshots/fixture.tgb</d:href></d:response></d:multistatus>`)
}

func TestStoreBasicAuthPrefixAndAtomicUpload(t *testing.T) {
	objects := map[string][]byte{}
	var objectMu sync.Mutex
	var moves atomic.Int32
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	store := newTestStore(t, server, Config{Endpoint: server.URL + "/dav", Username: "fixture-user", Password: "fixture-password", RemotePath: "root", TempDir: t.TempDir()})
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
	other := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(r.Header.Get("Authorization") != "")
		w.WriteHeader(http.StatusOK)
	}))
	other.URL = "http://other.example"
	redirect := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	store := newTestStore(t, redirect, Config{Endpoint: redirect.URL, Username: "fixture-user", Password: "fixture-password"})
	defer store.Close()
	if err := store.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect was not refused: %v", err)
	}
	if redirected.Load() {
		t.Fatal("Basic credentials reached cross-host redirect")
	}
	unauthorized := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	unauthorizedStore := newTestStore(t, unauthorized, Config{Endpoint: unauthorized.URL, Username: "fixture-user", Password: "fixture-password"})
	defer unauthorizedStore.Close()
	if err := unauthorizedStore.TestConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 not returned: %v", err)
	}
}

func TestStoreTimeoutCancellationAndUploadCleanup(t *testing.T) {
	var temporaryName string
	var puts atomic.Int32
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	store := newTestStore(t, server, Config{Endpoint: server.URL, Timeout: 50 * time.Millisecond, TempDir: t.TempDir()})
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
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	store := newTestStore(t, server, Config{Endpoint: server.URL, TempDir: t.TempDir()})
	defer store.Close()
	err := store.Upload(context.Background(), "tangentry/v1/snapshots/x.tgb", strings.NewReader("encrypted"))
	if err == nil || objects[tempPath] || moveCount.Load() != 0 {
		t.Fatalf("upload interruption leaked/finalized object: err=%v temp=%v moves=%d", err, objects[tempPath], moveCount.Load())
	}
}

func TestStorePathTraversalAndOversizedResponses(t *testing.T) {
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Length", fmt.Sprint(backup.MaxPackageSize+1))
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write(make([]byte, maxPropfind+1))
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL})
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
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL})
	defer store.Close()
	if err := store.TestConnection(context.Background()); err == nil {
		t.Fatal("permanent failure was accepted")
	}
	if requests.Load() != 1 {
		t.Fatalf("permanent HTTP failure retried %d times", requests.Load())
	}
}

func TestStoreConnectionTestRejectsOversizedUnknownLengthResponse(t *testing.T) {
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write(make([]byte, maxPropfind+1))
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL})
	defer store.Close()
	if err := store.TestConnection(context.Background()); !errors.Is(err, backup.ErrInvalidPackage) {
		t.Fatalf("oversized connection response was accepted: %v", err)
	}
}

func TestStoreReadRejectsOversizedUnknownLengthWhileStreaming(t *testing.T) {
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "0123456789abcdef")
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL})
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
	secure, err := New(Config{Endpoint: "https://fixture.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	if secure.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("self-signed certificate accepted by default")
	}
	insecure, err := New(Config{Endpoint: "https://fixture.invalid", AllowInsecureTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer insecure.Close()
	if !insecure.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("explicit self-signed option was ignored")
	}
	if tr := insecure.client.Transport.(*http.Transport); tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS minimum version changed")
	}
}

func TestBrowseReturnsSortedDirectChildFoldersAndDecodesChineseNames(t *testing.T) {
	var seenDepth string
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			t.Fatalf("method = %s", r.Method)
		}
		seenDepth = r.Header.Get("Depth")
		if r.URL.Path != "/dav/%E7%9B%AE%E5%BD%95/%E5%B5%8C%E5%A5%97" && r.URL.Path != "/dav/目录/嵌套" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, folderResponseXML(
			folderResponse("/dav/目录/嵌套/文档", ""),
			folderResponse("/dav/目录/嵌套/%E4%B8%AD%E6%96%87", "<d:collection/>"),
			folderResponse("/dav/目录/嵌套/alpha", "<d:collection/>"),
			folderResponse("/dav/目录/嵌套/z-parent/child", "<d:collection/>"),
			folderResponse("/dav/目录/嵌套/", "<d:collection/>"),
		))
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL + "/dav", RemotePath: "目录"})
	defer store.Close()
	folders, err := store.Browse(context.Background(), "嵌套")
	if err != nil {
		t.Fatal(err)
	}
	if seenDepth != "1" {
		t.Fatalf("Depth = %q, want 1", seenDepth)
	}
	want := []backup.RemoteFolder{{Name: "alpha", Path: "嵌套/alpha"}, {Name: "中文", Path: "嵌套/中文"}}
	if fmt.Sprint(folders) != fmt.Sprint(want) {
		t.Fatalf("folders = %#v, want %#v", folders, want)
	}
}

func TestBrowseRejectsTooManyFoldersOversizedResponseAndUnsafeHrefs(t *testing.T) {
	mode := "count"
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		switch mode {
		case "count":
			var entries strings.Builder
			for i := range maxFolders + 1 {
				fmt.Fprintf(&entries, folderResponse("/dav/folder-%03d", "<d:collection/>"), i)
			}
			_, _ = io.WriteString(w, folderResponseXML(entries.String()))
		case "size":
			_, _ = io.WriteString(w, strings.Repeat("x", maxPropfind+1))
		case "unsafe":
			_, _ = io.WriteString(w, folderResponseXML(folderResponse("https://other.invalid/dav/escape", "<d:collection/>")))
		}
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL + "/dav"})
	defer store.Close()
	if _, err := store.Browse(context.Background(), ""); !errors.Is(err, backup.ErrTooManyFolders) {
		t.Fatalf("folder count limit: %v", err)
	}
	mode = "size"
	if _, err := store.Browse(context.Background(), ""); !errors.Is(err, backup.ErrFolderResponseTooLarge) {
		t.Fatalf("response size limit: %v", err)
	}
	mode = "unsafe"
	if _, err := store.Browse(context.Background(), ""); err == nil {
		t.Fatal("cross-host href accepted")
	}
}

func TestCreateFolderClassifiesSuccessExistsPermissionAndMissingParent(t *testing.T) {
	mode := "created"
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			switch mode {
			case "created":
				w.WriteHeader(http.StatusCreated)
			case "exists":
				w.WriteHeader(http.StatusMethodNotAllowed)
			case "missing":
				w.WriteHeader(http.StatusConflict)
			case "forbidden":
				w.WriteHeader(http.StatusForbidden)
			case "unauthorized":
				w.WriteHeader(http.StatusUnauthorized)
			}
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			if mode == "missing" {
				_, _ = io.WriteString(w, folderResponseXML())
			} else if mode == "exists" && r.Header.Get("Depth") == "0" && r.URL.Path == "/dav/parent/New" {
				_, _ = io.WriteString(w, folderResponseXML(folderResponse(r.URL.Path, "<d:collection/>")))
			} else if mode == "missing" && r.URL.Path == "/dav/parent" {
				_, _ = io.WriteString(w, folderResponseXML())
			} else {
				_, _ = io.WriteString(w, folderResponseXML(folderResponse(r.URL.Path, "<d:collection/>")))
			}
		}
	}))
	store := newTestStore(t, server, Config{Endpoint: server.URL + "/dav"})
	defer store.Close()
	if exists, err := store.CreateFolder(context.Background(), "parent", "New"); err != nil || exists {
		t.Fatalf("create: exists=%v err=%v", exists, err)
	}
	mode = "exists"
	if exists, err := store.CreateFolder(context.Background(), "parent", "New"); err != nil || !exists {
		t.Fatalf("existing folder: exists=%v err=%v", exists, err)
	}
	mode = "missing"
	if _, err := store.CreateFolder(context.Background(), "parent", "New"); !errors.Is(err, backup.ErrFolderParentNotFound) {
		t.Fatalf("missing parent: %v", err)
	}
	mode = "forbidden"
	if _, err := store.CreateFolder(context.Background(), "parent", "New"); !errors.Is(err, backup.ErrFolderPermissionDenied) {
		t.Fatalf("forbidden: %v", err)
	}
	mode = "unauthorized"
	if _, err := store.CreateFolder(context.Background(), "parent", "New"); !errors.Is(err, backup.ErrFolderUnauthorized) {
		t.Fatalf("unauthorized: %v", err)
	}
}

func TestFolderPathsAndNamesAreValidatedBeforeRemoteRequest(t *testing.T) {
	var requests atomic.Int32
	server := newIPv4DAVServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusCreated) }))
	store := newTestStore(t, server, Config{Endpoint: server.URL})
	defer store.Close()
	for _, folderPath := range []string{"../outside", "%2e%2e/outside", "a//b", "/absolute", `a\\b`} {
		if _, err := store.Browse(context.Background(), folderPath); !errors.Is(err, backup.ErrInvalidFolderPath) {
			t.Fatalf("path %q accepted: %v", folderPath, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", `a\\b`, "bad\nname"} {
		if _, err := store.CreateFolder(context.Background(), "", name); !errors.Is(err, backup.ErrInvalidFolderName) {
			t.Fatalf("name %q accepted: %v", name, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid paths made %d requests", requests.Load())
	}
}
