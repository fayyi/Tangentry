package xiaohongshu

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func pictureFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagesKeepPublicPictureAfterReopenWithoutAccountOrNetwork(t *testing.T) {
	root := t.TempDir()
	picture := pictureFixture(t)
	calls := 0
	cache, err := OpenImages(root, &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("Referer") != "https://www.xiaohongshu.com/" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatal("unexpected image headers")
		}
		// A misleading upstream content type cannot make executable content
		// run under Tangentry's origin.
		return response(req, 200, string(picture), http.Header{"Content-Type": {"text/html"}}), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	const raw = "https://sns-webpic-qc.xhscdn.com/known-picture"
	data, err := cache.Read(context.Background(), raw)
	if err != nil || data.Type != "image/png" || !bytes.Equal(data.Bytes, picture) {
		t.Fatal("picture not fetched and saved", err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 {
		t.Fatal("unexpected cache files", err)
	}
	info, err := files[0].Info()
	if err != nil || info.Mode().Perm() != 0600 || strings.Contains(files[0].Name(), "known-picture") {
		t.Fatal("cache should be private and opaque", err)
	}
	reopened, err := OpenImages(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err = reopened.Read(context.Background(), raw)
	if err != nil || !bytes.Equal(data.Bytes, picture) || calls != 1 {
		t.Fatal("cached picture depended on a platform connection", err, calls)
	}
}

func TestImagesRejectUntrustedSourcesErrorsAndExecutableBodiesWithoutRetries(t *testing.T) {
	picture := pictureFixture(t)
	for _, tc := range []struct {
		raw    string
		status int
		body   string
		calls  int
	}{
		{"https://evil.example/picture", 200, string(picture), 0},
		{"https://xhscdn.com.evil.example/picture", 200, string(picture), 0},
		{"https://user:private@sns-webpic-qc.xhscdn.com/picture", 200, string(picture), 0},
		{"https://sns-webpic-qc.xhscdn.com:8443/picture", 200, string(picture), 0},
		{"http://sns-webpic-qc.xhscdn.com/picture", 200, string(picture), 0},
		{"https://sns-webpic-qc.xhscdn.com/picture", 403, "private upstream error", 1},
		{"https://sns-webpic-qc.xhscdn.com/picture", 429, "private upstream error", 1},
		{"https://sns-webpic-qc.xhscdn.com/picture", 302, "", 1},
		{"https://sns-webpic-qc.xhscdn.com/picture", 200, "<svg><script>run()</script></svg>", 1},
		{"https://sns-webpic-qc.xhscdn.com/picture", 200, "<html>private login</html>", 1},
		{"https://sns-webpic-qc.xhscdn.com/picture", 200, string(picture) + strings.Repeat("a", maxImageBytes), 1},
	} {
		calls := 0
		root := t.TempDir()
		cache, err := OpenImages(root, &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			return response(req, tc.status, tc.body, http.Header{"Location": {"https://evil.example/private"}}), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cache.Read(context.Background(), tc.raw); err == nil || strings.Contains(err.Error(), "private") || calls != tc.calls {
			t.Fatal("unsafe image accepted or retried", tc.status, err, calls)
		}
		files, _ := os.ReadDir(root)
		if len(files) != 0 {
			t.Fatal("failed image retained")
		}
	}
}

func TestConcurrentPicturesShareOneFetchAndCanceledWaiterDoesNotCancelOwner(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	cache, err := OpenImages(t.TempDir(), &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return response(req, 200, string(pictureFixture(t)), nil), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	const raw = "https://sns-webpic-qc.xhscdn.com/shared"
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := cache.Read(context.Background(), raw); err != nil {
			t.Error(err)
		}
	})
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Read(ctx, raw); err != context.Canceled {
		t.Fatal("canceled waiter continued", err)
	}
	close(release)
	wg.Wait()
	if _, err := cache.Read(context.Background(), raw); err != nil || calls.Load() != 1 {
		t.Fatal("picture was fetched twice", err, calls.Load())
	}
}

func TestImageCacheRejectsSymlinksAndCanReplaceDamagedData(t *testing.T) {
	const raw = "https://sns-webpic-qc.xhscdn.com/local"
	root := t.TempDir()
	path := filepath.Join(root, ImageVersion(raw)+".img")
	target := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(target, pictureFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	cache, _ := OpenImages(root, nil)
	if _, err := cache.Read(context.Background(), raw); err == nil {
		t.Fatal("symlink was read")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	cache, _ = OpenImages(root, &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		return response(req, 200, string(pictureFixture(t)), nil), nil
	})})
	if _, err := cache.Read(context.Background(), raw); err != nil {
		t.Fatal("damaged picture could not be repaired", err)
	}
}
