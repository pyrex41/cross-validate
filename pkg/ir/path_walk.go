package ir

import (
	"strconv"
	"strings"
)

// PathHit is a single expanded path match: the concrete path (with array
// indices substituted) and the value at that path.
type PathHit struct {
	// Path is the concrete path with wildcards replaced by their index.
	// E.g. "xs[*].k" hitting a 2-element array produces "xs[0].k" and "xs[1].k".
	Path string
	// Value is the decoded YAML/JSON value at the resolved path.
	Value interface{}
}

// WalkPath walks a JSON-path expression against a raw map, returning one
// PathHit per expansion. Supports:
//   - scalar dotted paths ("a.b.c")
//   - explicit indices ("a.b[0].c")
//   - wildcards ("a.b[*].c" or "a.b[].c" — both treated as "match any element")
//
// Wildcards emit one hit per array element; explicit indices emit zero or one
// hit. Returns nil if the path doesn't resolve.
//
// The returned Path substitutes concrete indices for wildcard segments so
// callers can pair hits from two parallel walks (e.g. SelectorPath vs
// ResolvedPath) by their "[N]" suffix signature.
func WalkPath(raw map[string]interface{}, path string) []PathHit {
	if raw == nil || path == "" {
		return nil
	}
	segments, err := parsePath(path)
	if err != nil {
		return nil
	}
	return walkSegments(segments, 0, "", raw)
}

// pathSegment represents one step in a parsed path. Exactly one of the three
// fields is populated per segment.
type pathSegment struct {
	// key is a named field (e.g. "spec"); empty when the segment is an index
	// or wildcard.
	key string
	// index is a concrete array index (>=0). Only meaningful when isIndex.
	index int
	// isIndex is true when this segment indexes into an array.
	isIndex bool
	// isWildcard is true when this segment matches every element of an array.
	// When true, isIndex is also true.
	isWildcard bool
}

// parsePath splits a path like "spec.xs[0].k" or "spec.xs[*].k" into an
// ordered sequence of segments. An empty-bracket "[]" is treated as "[*]".
func parsePath(path string) ([]pathSegment, error) {
	var out []pathSegment
	// Split on "." first; each chunk may still contain bracket segments.
	parts := strings.Split(path, ".")
	for _, p := range parts {
		if p == "" {
			// Consecutive dots or leading/trailing dot — treat as malformed.
			return nil, errInvalidPath
		}
		// Find any "[" — the prefix is a key, the suffix is a sequence of
		// bracket expressions.
		if i := strings.Index(p, "["); i >= 0 {
			if i > 0 {
				out = append(out, pathSegment{key: p[:i]})
			}
			rest := p[i:]
			for len(rest) > 0 {
				if !strings.HasPrefix(rest, "[") {
					return nil, errInvalidPath
				}
				end := strings.Index(rest, "]")
				if end < 0 {
					return nil, errInvalidPath
				}
				inside := rest[1:end]
				seg := pathSegment{isIndex: true}
				switch inside {
				case "", "*":
					seg.isWildcard = true
				default:
					n, err := strconv.Atoi(inside)
					if err != nil || n < 0 {
						return nil, errInvalidPath
					}
					seg.index = n
				}
				out = append(out, seg)
				rest = rest[end+1:]
			}
		} else {
			out = append(out, pathSegment{key: p})
		}
	}
	return out, nil
}

// walkSegments recursively resolves the remaining segments against val,
// emitting one PathHit per complete resolution. concretePath tracks the
// rendered path so far (with wildcard indices substituted).
func walkSegments(segments []pathSegment, idx int, concretePath string, val interface{}) []PathHit {
	if idx == len(segments) {
		if val == nil {
			return nil
		}
		return []PathHit{{Path: concretePath, Value: val}}
	}
	seg := segments[idx]
	if seg.isIndex {
		arr, ok := val.([]interface{})
		if !ok {
			return nil
		}
		if seg.isWildcard {
			var hits []PathHit
			for i, el := range arr {
				sub := concretePath + "[" + strconv.Itoa(i) + "]"
				hits = append(hits, walkSegments(segments, idx+1, sub, el)...)
			}
			return hits
		}
		if seg.index >= len(arr) {
			return nil
		}
		sub := concretePath + "[" + strconv.Itoa(seg.index) + "]"
		return walkSegments(segments, idx+1, sub, arr[seg.index])
	}
	// Named key: val must be a map.
	m, ok := val.(map[string]interface{})
	if !ok {
		return nil
	}
	next, present := m[seg.key]
	if !present {
		return nil
	}
	var sub string
	if concretePath == "" {
		sub = seg.key
	} else {
		sub = concretePath + "." + seg.key
	}
	return walkSegments(segments, idx+1, sub, next)
}

// ReadPath resolves a dotted path against a raw YAML/JSON map and returns
// the value at that location. Returns (nil, false) when any path segment is
// missing or an intermediate segment is not a map.
//
// Unlike WalkPath, ReadPath is a single-result lookup: it takes only named
// keys separated by "." and does NOT support array indexing or wildcards.
// The final segment's value is returned verbatim (scalar, map, or slice).
//
// Used by R27 (XPC.P.immutable-change) for scalar-leaf registry lookups
// against base/head raw maps.
func ReadPath(raw map[string]interface{}, dottedPath string) (interface{}, bool) {
	if raw == nil || dottedPath == "" {
		return nil, false
	}
	segments := strings.Split(dottedPath, ".")
	var cur interface{} = raw
	for i, seg := range segments {
		if seg == "" {
			return nil, false
		}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, present := m[seg]
		if !present {
			return nil, false
		}
		if i == len(segments)-1 {
			return next, true
		}
		cur = next
	}
	return nil, false
}

// errInvalidPath is a sentinel used to signal malformed path expressions;
// callers treat it as "no hits" rather than propagating.
var errInvalidPath = &pathError{"invalid path"}

type pathError struct{ msg string }

func (e *pathError) Error() string { return e.msg }
