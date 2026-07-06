package main

import (
	"path/filepath"
	"testing"
)

func TestCommandChecksumStable(t *testing.T) {
	a := commandChecksum([]string{"--llm", "--level", "error"})
	b := commandChecksum([]string{"--llm", "--level", "error"})
	c := commandChecksum([]string{"--llm", "--level", "warn"})
	if a != b {
		t.Error("same args should produce the same checksum")
	}
	if a == c {
		t.Error("different args should produce different checksums")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "sub", commandChecksum([]string{"--llm"}))

	writer := newStore(nil, false, cacheFile)
	writer.set("time,severity,msg", &Template{
		TimestampPath: "time",
		LevelPath:     "severity",
		MessagePath:   "msg",
		Highlights:    []Highlight{{Path: "host", Label: "host", Color: "cyan"}},
	})

	reader := newStore(nil, false, cacheFile)
	got := reader.Get("time,severity,msg")
	if got == nil {
		t.Fatal("template not loaded from cache")
	}
	if got.LevelPath != "severity" || got.source != "cache" {
		t.Errorf("unexpected cached template: %+v (source=%s)", got, got.source)
	}
	if len(got.Highlights) != 1 || got.Highlights[0].Path != "host" {
		t.Errorf("highlights not round-tripped: %+v", got.Highlights)
	}
	if sigs := reader.cachedSignatures(); len(sigs) != 1 {
		t.Errorf("cachedSignatures = %v, want 1", sigs)
	}
}
