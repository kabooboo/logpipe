package main

import (
	"sort"
	"strings"
)

// flatten turns a decoded JSON object into a map of dot-path -> leaf value.
// Nested objects are recursed into; arrays and scalars are kept as-is at their path.
func flatten(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	flattenInto(out, "", m)
	return out
}

func flattenInto(out map[string]interface{}, prefix string, m map[string]interface{}) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if child, ok := v.(map[string]interface{}); ok {
			if len(child) == 0 {
				out[path] = child
				continue
			}
			flattenInto(out, path, child)
			continue
		}
		out[path] = v
	}
}

// topLevelKeys returns the sorted set of first-segment keys present in a flattened map.
func topLevelKeys(flat map[string]interface{}) []string {
	seen := make(map[string]struct{})
	for path := range flat {
		key := path
		if i := strings.IndexByte(path, '.'); i >= 0 {
			key = path[:i]
		}
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
