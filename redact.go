package main

import (
	"fmt"
	"regexp"
	"strings"
)

var secretKeyRe = regexp.MustCompile(`(?i)pass|secret|token|api[_-]?key|authorization|credential`)

// isSecretPath reports whether the last segment of a dot-path names a secret.
func isSecretPath(path string) bool {
	last := path
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		last = path[i+1:]
	}
	return secretKeyRe.MatchString(last)
}

// redactFlat masks secret-keyed leaf values in place so they never reach the
// rendered output, the LLM request, or the profile samples.
func redactFlat(flat map[string]interface{}, redact bool) {
	if !redact {
		return
	}
	for path, v := range flat {
		if !isSecretPath(path) {
			continue
		}
		if _, isObj := v.(map[string]interface{}); isObj {
			continue
		}
		flat[path] = "***"
	}
}

// sampleValue renders a leaf value to a short string for profiling, masking
// secret-keyed values when redact is enabled.
func sampleValue(path string, v interface{}, redact bool) string {
	if redact && isSecretPath(path) {
		return "***"
	}
	s := scalarString(v)
	if len(s) > maxSampleLen {
		s = s[:maxSampleLen] + "…"
	}
	return s
}

// scalarString formats a JSON leaf value without Go type noise.
func scalarString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
