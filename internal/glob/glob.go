// Package glob matches slash-separated paths against globs with "**" support.
package glob

import (
	"path"
	"strings"
)

// Match reports whether the slash-separated name matches glob.
// A "**" segment matches zero or more path segments, except a trailing "**",
// which needs at least one; every other segment is matched with path.Match,
// so "*" and "?" never cross a "/". Malformed patterns match nothing (config
// validates glob syntax up front).
//
// Runs of "**" are collapsed and matching is memoized on (pattern index,
// name index), so the cost is bounded by pattern segments times name segments
// even for hostile patterns.
func Match(glob, name string) bool {
	pattern := collapseDoubleStars(strings.Split(glob, "/"))
	names := strings.Split(name, "/")
	// memo[p][n]: 0 = unknown, 1 = no match, 2 = match.
	memo := make([][]uint8, len(pattern)+1)
	for i := range memo {
		memo[i] = make([]uint8, len(names)+1)
	}
	return matchSegments(pattern, names, 0, 0, memo)
}

func collapseDoubleStars(segments []string) []string {
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "**" && len(out) > 0 && out[len(out)-1] == "**" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func matchSegments(pattern, names []string, p, n int, memo [][]uint8) bool {
	if memo[p][n] != 0 {
		return memo[p][n] == 2
	}
	result := matchSegmentsUncached(pattern, names, p, n, memo)
	memo[p][n] = 1
	if result {
		memo[p][n] = 2
	}
	return result
}

func matchSegmentsUncached(pattern, names []string, p, n int, memo [][]uint8) bool {
	if p == len(pattern) {
		return n == len(names)
	}
	if pattern[p] == "**" {
		if p == len(pattern)-1 {
			return n < len(names)
		}
		for i := n; i <= len(names); i++ {
			if matchSegments(pattern, names, p+1, i, memo) {
				return true
			}
		}
		return false
	}
	if n == len(names) {
		return false
	}
	ok, err := path.Match(pattern[p], names[n])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern, names, p+1, n+1, memo)
}
