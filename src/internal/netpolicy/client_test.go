package netpolicy

import (
	"context"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func TestPublicFetchRejectsPrivateNetworks(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "192.168.1.1", "100.100.100.200", "2001:db8::1", "64:ff9b::7f00:1"} {
		if public(netip.MustParseAddr(raw)) {
			t.Fatal("private accepted", raw)
		}
	}
	if !public(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1", nil)
	if res, err := NewClient().Do(req); err == nil {
		res.Body.Close()
		t.Fatal("network boundary allowed local host")
	}
}
