// Package htmlimage picks the displayable address of an HTML <img>.
//
// Feeds and original articles both store images as links, never bytes; the
// reader loads them through the local image proxy.
package htmlimage

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"tangentry/internal/netpolicy"
)

// Lazy-loading pages keep a placeholder in src and the real image in a data
// attribute, so the data attributes win.
var sourceAttrs = []string{"data-src", "data-original", "data-lazy-src", "data-actualsrc", "data-image-src", "data-fallback-src", "data-url", "src"}

// Attrs returns an element's attributes with lower-case keys.
func Attrs(node *html.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[strings.ToLower(attr.Key)] = strings.TrimSpace(attr.Val)
	}
	return attrs
}

// Source returns the raw image address of an <img>'s attributes, or "".
func Source(attrs map[string]string) string {
	for _, key := range sourceAttrs {
		value := attrs[key]
		lower := strings.ToLower(value)
		if value == "" || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "javascript:") {
			continue
		}
		return value
	}
	for _, key := range []string{"data-srcset", "srcset"} {
		if value := attrs[key]; value != "" {
			return strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	return ""
}

// Resolve makes raw absolute against base and returns it only when it is a
// public http(s) address.
func Resolve(raw, base string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	raw = fields[0]
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(strings.ToLower(raw), "upload://") {
		key := strings.TrimPrefix(raw, "upload://")
		key = strings.TrimPrefix(key, "/")
		if key == "" {
			return ""
		}
		raw = "https://linux.do/uploads/short-url/" + key
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() && base != "" {
		parent, err := url.Parse(base)
		if err != nil {
			return ""
		}
		u = parent.ResolveReference(u)
	}
	value, err := netpolicy.URL(u.String())
	if err != nil || local(u.Hostname()) {
		return ""
	}
	return value
}

// local reports hosts that can never be public; the proxy's dialer refuses
// them anyway, so such links are not worth storing.
func local(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

// URL returns the displayable absolute address of an <img>, or "".
func URL(node *html.Node, base string) string {
	return Resolve(Source(Attrs(node)), base)
}

// Dimension parses a width or height attribute; invalid values become 0.
func Dimension(raw string) int {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(raw), "px"))
	if err != nil || n < 0 || n > 30000 {
		return 0
	}
	return n
}
