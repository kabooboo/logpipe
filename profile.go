package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxClusters      = 32
	maxSamplesPerKey = 3
	maxSampleLen     = 80
	overflowSig      = "__overflow__"
)

// signature identifies a log "format" by its sorted set of top-level keys.
func signature(flat map[string]interface{}) string {
	return strings.Join(topLevelKeys(flat), ",")
}

// FieldStat aggregates what has been observed at a single dot-path.
type FieldStat struct {
	Count   int
	Types   map[string]int
	Samples []string
}

// Profile accumulates the observed shape of one log format (cluster).
type Profile struct {
	Signature string
	Count     int
	Fields    map[string]*FieldStat

	lastGenPaths int
	lastGenCount int
}

func newProfile(sig string) *Profile {
	return &Profile{Signature: sig, Fields: make(map[string]*FieldStat)}
}

// Observe folds one flattened record into the profile.
func (p *Profile) Observe(flat map[string]interface{}, redact bool) {
	p.Count++
	for path, v := range flat {
		fs := p.Fields[path]
		if fs == nil {
			fs = &FieldStat{Types: make(map[string]int)}
			p.Fields[path] = fs
		}
		fs.Count++
		fs.Types[jsonType(v)]++

		if len(fs.Samples) < maxSamplesPerKey {
			s := sampleValue(path, v, redact)
			if s != "" && !contains(fs.Samples, s) {
				fs.Samples = append(fs.Samples, s)
			}
		}
	}
}

// ProfileSnapshot is an immutable copy handed to the background LLM worker.
type ProfileSnapshot struct {
	Signature string
	Count     int
	Fields    map[string]FieldStat
}

func (p *Profile) Snapshot() ProfileSnapshot {
	fields := make(map[string]FieldStat, len(p.Fields))
	for path, fs := range p.Fields {
		types := make(map[string]int, len(fs.Types))
		for t, n := range fs.Types {
			types[t] = n
		}
		samples := append([]string(nil), fs.Samples...)
		fields[path] = FieldStat{Count: fs.Count, Types: types, Samples: samples}
	}
	return ProfileSnapshot{Signature: p.Signature, Count: p.Count, Fields: fields}
}

// paths returns the profile's field paths, sorted, for stable prompts.
func (s ProfileSnapshot) paths() []string {
	paths := make([]string, 0, len(s.Fields))
	for p := range s.Fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// Profiles routes records to per-signature profiles with a bounded cluster count.
type Profiles struct {
	bySig          map[string]*Profile
	redact         bool
	overflowWarned bool
}

func newProfiles(redact bool) *Profiles {
	return &Profiles{bySig: make(map[string]*Profile), redact: redact}
}

// route returns the profile for a record, collapsing to a shared overflow
// profile once the cluster cap is hit (warned once on stderr).
func (ps *Profiles) route(sig string, flat map[string]interface{}) *Profile {
	if p := ps.bySig[sig]; p != nil {
		p.Observe(flat, ps.redact)
		return p
	}
	if len(ps.bySig) >= maxClusters {
		if !ps.overflowWarned {
			fmt.Fprintf(stderr, "logpipe: cluster cap (%d) reached; further formats share one generic renderer\n", maxClusters)
			ps.overflowWarned = true
		}
		p := ps.bySig[overflowSig]
		if p == nil {
			p = newProfile(overflowSig)
			ps.bySig[overflowSig] = p
		}
		p.Observe(flat, ps.redact)
		return p
	}
	p := newProfile(sig)
	ps.bySig[sig] = p
	p.Observe(flat, ps.redact)
	return p
}

func jsonType(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
