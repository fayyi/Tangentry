package tools

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Expose only a bounded duration, never the raw upstream header.
func speechRetryHint(header string, now time.Time) string {
	if len(header) > 128 {
		return ""
	}
	header = strings.TrimSpace(header)
	seconds, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		date, parseErr := http.ParseTime(header)
		if parseErr != nil {
			return ""
		}
		seconds = int64(math.Ceil(date.Sub(now).Seconds()))
	}
	if seconds <= 0 || seconds > 7*24*3600 {
		return ""
	}
	if seconds < 60 {
		return fmt.Sprintf("平台建议至少等待 %d 秒。", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("平台建议至少等待 %d 分钟。", (seconds+59)/60)
	}
	return fmt.Sprintf("平台建议至少等待 %d 小时。", (seconds+3599)/3600)
}
