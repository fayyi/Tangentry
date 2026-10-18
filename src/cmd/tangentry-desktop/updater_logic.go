package main

import (
	"strconv"
	"strings"
	"time"
)

const updaterInterval = 6 * time.Hour

func updaterCheckDue(last, now time.Time, interval time.Duration) bool {
	if last.IsZero() {
		return true
	}
	return !now.Before(last.Add(interval))
}

func updaterShouldSkip(version, skipped string) bool {
	return version != "" && version == skipped
}

// updaterVersionNewer is used by preference tests and keeps version decisions
// deterministic without involving a network request. MyGo performs the same
// comparison for signed manifests.
func updaterVersionNewer(candidate, current string) bool {
	return compareUpdaterVersions(candidate, current) > 0
}

func compareUpdaterVersions(a, b string) int {
	a, b = strings.TrimPrefix(strings.TrimSpace(a), "v"), strings.TrimPrefix(strings.TrimSpace(b), "v")
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	acore, apre, _ := strings.Cut(a, "-")
	bcore, bpre, _ := strings.Cut(b, "-")
	if comparison := compareUpdaterFields(acore, bcore); comparison != 0 {
		return comparison
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	default:
		return compareUpdaterFields(apre, bpre)
	}
}

func compareUpdaterFields(a, b string) int {
	aParts, bParts := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(aParts), len(bParts)); i++ {
		var ap, bp string
		if i < len(aParts) {
			ap = aParts[i]
		}
		if i < len(bParts) {
			bp = bParts[i]
		}
		switch {
		case ap == bp:
			continue
		case ap == "":
			return -1
		case bp == "":
			return 1
		}
		an, aerr := strconv.Atoi(ap)
		bn, berr := strconv.Atoi(bp)
		switch {
		case aerr == nil && berr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			return strings.Compare(ap, bp)
		}
	}
	return 0
}
