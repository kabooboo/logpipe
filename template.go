package main

import (
	"sort"
	"strings"
)

// Highlight is one field the renderer prints after the message.
type Highlight struct {
	Path   string `json:"path"`
	Label  string `json:"label"`  // "" prints bare value; else "label=value"
	Color  string `json:"color"`  // e.g. "green", "red+bold", "status"
	Format string `json:"format"` // "" | "ms-from-ns" | "truncate:N" | "status" | "bytes"
}

// Template describes how to render one log format.
type Template struct {
	Signature     string      `json:"-"`
	TimestampPath string      `json:"timestamp_path"`
	LevelPath     string      `json:"level_path"`
	MessagePath   string      `json:"message_path"`
	Highlights    []Highlight `json:"highlights"`
	source        string      // "heuristic" | "llm"
}

var (
	timestampCandidates = []string{"@timestamp", "timestamp", "ts", "@t", "time", "date"}
	levelCandidates     = []string{"log.level", "level", "severity", "lvl", "loglevel"}
	messageCandidates   = []string{"message", "msg", "text", "log.original"}
)

// knownHighlight maps a well-known ECS-ish path to a preferred rendering.
type knownHighlight struct {
	path  string
	label string
	color string
	fmt   string
}

var knownHighlights = []knownHighlight{
	{"http.request.method", "", "magenta+bold", ""},
	{"http.response.status_code", "", "status", "status"},
	{"url.path", "", "green", ""},
	{"source.ip", "from", "blue", ""},
	{"event.duration", "", "yellow", "ms-from-ns"},
	{"user_agent.original", "ua", "blue", "truncate:50"},
	{"error", "error", "red+bold", ""},
}

const maxHighlights = 6

// buildHeuristic builds a render template from a profile's live field map with
// no network access.
func buildHeuristic(sig string, fields map[string]*FieldStat) *Template {
	t := &Template{Signature: sig, source: "heuristic"}
	t.TimestampPath = firstPresent(fields, timestampCandidates)
	t.LevelPath = firstPresent(fields, levelCandidates)
	t.MessagePath = firstPresent(fields, messageCandidates)

	used := map[string]bool{
		t.TimestampPath: true, t.LevelPath: true, t.MessagePath: true,
	}

	for _, kh := range knownHighlights {
		if _, ok := fields[kh.path]; ok && !used[kh.path] {
			t.Highlights = append(t.Highlights, Highlight{kh.path, kh.label, kh.color, kh.fmt})
			used[kh.path] = true
		}
	}

	if len(t.Highlights) < maxHighlights {
		for _, path := range sortedPaths(fields) {
			if used[path] || len(t.Highlights) >= maxHighlights {
				continue
			}
			if !isScalar(*fields[path]) {
				continue
			}
			t.Highlights = append(t.Highlights, Highlight{
				Path:  path,
				Label: leafKey(path),
				Color: "white",
			})
			used[path] = true
		}
	}
	return t
}

func firstPresent(fields map[string]*FieldStat, candidates []string) string {
	for _, c := range candidates {
		if _, ok := fields[c]; ok {
			return c
		}
	}
	return ""
}

func sortedPaths(fields map[string]*FieldStat) []string {
	paths := make([]string, 0, len(fields))
	for p := range fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// isScalar reports whether a field only ever held scalar (non object/array) values.
func isScalar(fs FieldStat) bool {
	if fs.Types == nil {
		return false
	}
	for t := range fs.Types {
		if t == "object" || t == "array" {
			return false
		}
	}
	return true
}

func leafKey(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// fieldValue reads a leaf value from a flattened record.
func fieldValue(flat map[string]interface{}, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}
	v, ok := flat[path]
	return v, ok
}

// resolveString returns the string form of the value at path, or "".
func resolveString(flat map[string]interface{}, path string) string {
	if v, ok := fieldValue(flat, path); ok {
		return scalarString(v)
	}
	return ""
}

// validate returns a copy with any Highlight/path absent from the profile removed,
// and reports whether the core paths are all present in the profile.
func (t *Template) validate(snap ProfileSnapshot) bool {
	present := func(p string) bool {
		if p == "" {
			return true
		}
		_, ok := snap.Fields[p]
		return ok
	}
	if !present(t.TimestampPath) || !present(t.LevelPath) || !present(t.MessagePath) {
		return false
	}
	kept := t.Highlights[:0]
	for _, h := range t.Highlights {
		if _, ok := snap.Fields[h.Path]; ok {
			kept = append(kept, h)
		}
	}
	t.Highlights = kept
	return true
}
