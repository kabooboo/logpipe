package main

// Highlight is one field the renderer prints after the message.
type Highlight struct {
	Path   string `json:"path"`
	Label  string `json:"label"`  // "" prints bare value; else "label=value"
	Color  string `json:"color"`  // e.g. "green", "red+bold", "status"
	Format string `json:"format"` // "" | "ms-from-ns" | "truncate:N" | "status" | "bytes"
}

// Template describes how to render one log format. All templates come from the
// LLM (fresh) or the on-disk cache; there is no built-in schema.
type Template struct {
	Signature     string      `json:"-"`
	TimestampPath string      `json:"timestamp_path"`
	LevelPath     string      `json:"level_path"`
	MessagePath   string      `json:"message_path"`
	Highlights    []Highlight `json:"highlights"`
	source        string      // "llm" | "cache"
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

// validate drops highlights whose path is absent from the profile and reports
// whether the core paths are all present.
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
