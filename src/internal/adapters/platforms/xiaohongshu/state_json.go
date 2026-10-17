package xiaohongshu

import (
	"encoding/json"
	"strings"

	"tangentry/internal/discovery"
)

// Hydration adds undefined and empty collection literals to JSON. Normalize
// these exact data forms outside strings; never evaluate page scripts.
func stateJSON(script string) ([]byte, error) {
	var out strings.Builder
	quoted, escaped := false, false
	depth := 0
	if !strings.HasPrefix(script, "{") {
		return nil, discovery.ErrUnavailable
	}
	for i := 0; i < len(script); i++ {
		ch := script[i]
		if quoted {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == '"' {
			quoted = true
			out.WriteByte(ch)
			continue
		}
		if strings.HasPrefix(script[i:], "undefined") {
			out.WriteString("null")
			i += len("undefined") - 1
			continue
		}
		emptyCollection := ""
		for _, literal := range []string{"new Map()", "new Map([])", "new Set()", "new Set([])"} {
			if strings.HasPrefix(script[i:], literal) {
				emptyCollection = literal
				break
			}
		}
		if emptyCollection != "" {
			if strings.HasPrefix(emptyCollection, "new Map") {
				out.WriteString("{}")
			} else {
				out.WriteString("[]")
			}
			i += len(emptyCollection) - 1
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
		}
		if ch == '}' || ch == ']' {
			depth--
		}
		out.WriteByte(ch)
		if depth == 0 {
			tail := strings.TrimSpace(script[i+1:])
			if tail != "" && tail != ";" {
				return nil, discovery.ErrUnavailable
			}
			if !json.Valid([]byte(out.String())) {
				return nil, discovery.ErrUnavailable
			}
			return []byte(out.String()), nil
		}
	}
	return nil, discovery.ErrUnavailable
}
