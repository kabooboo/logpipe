package main

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func mustFlatten(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return flatten(m)
}

func TestFlatten(t *testing.T) {
	flat := mustFlatten(t, `{"@timestamp":"t","log.level":"info","http":{"request":{"method":"GET"}},"tags":["a","b"]}`)
	cases := map[string]interface{}{
		"@timestamp":          "t",
		"log.level":           "info",
		"http.request.method": "GET",
	}
	for path, want := range cases {
		if got, ok := flat[path]; !ok || got != want {
			t.Errorf("flat[%q] = %v (ok=%v), want %v", path, got, ok, want)
		}
	}
	if _, ok := flat["tags"]; !ok {
		t.Error("array field should be kept as a leaf")
	}
}

func TestSignatureGroupsByTopLevelKeys(t *testing.T) {
	httpLog := mustFlatten(t, `{"@timestamp":"t","http":{"request":{"method":"GET"}},"url":{"path":"/x"}}`)
	errLog := mustFlatten(t, `{"@timestamp":"t","error":{"code":"X"}}`)
	if signature(httpLog) == signature(errLog) {
		t.Error("different top-level shapes should have different signatures")
	}
	httpLog2 := mustFlatten(t, `{"@timestamp":"u","http":{"request":{"method":"POST"}},"url":{"path":"/y"}}`)
	if signature(httpLog) != signature(httpLog2) {
		t.Error("same top-level shape should share a signature")
	}
}

func TestProfileObserveAndRedaction(t *testing.T) {
	p := newProfile("sig")
	p.Observe(mustFlatten(t, `{"level":"info","user":{"password":"hunter2"}}`), true)
	p.Observe(mustFlatten(t, `{"level":"warn","user":{"password":"hunter2"}}`), true)

	if p.Count != 2 {
		t.Errorf("Count = %d, want 2", p.Count)
	}
	if p.Fields["level"].Count != 2 {
		t.Errorf("level count = %d, want 2", p.Fields["level"].Count)
	}
	pw := p.Fields["user.password"]
	if pw == nil || len(pw.Samples) == 0 || pw.Samples[0] != "***" {
		t.Errorf("secret value should be masked, got %+v", pw)
	}
}

func TestRedactFlat(t *testing.T) {
	flat := mustFlatten(t, `{"message":"ok","password":"hunter2","http":{"request":{"authorization":"Bearer x"}},"count":5}`)
	redactFlat(flat, true)
	if flat["password"] != "***" {
		t.Errorf("password not masked: %v", flat["password"])
	}
	if flat["http.request.authorization"] != "***" {
		t.Errorf("nested secret not masked: %v", flat["http.request.authorization"])
	}
	if flat["message"] != "ok" || flat["count"].(float64) != 5 {
		t.Error("non-secret fields should be untouched")
	}

	flat2 := mustFlatten(t, `{"password":"hunter2"}`)
	redactFlat(flat2, false)
	if flat2["password"] != "hunter2" {
		t.Error("redact=false should leave values intact")
	}
}

func TestHeuristicTemplateECS(t *testing.T) {
	p := newProfile("sig")
	p.Observe(mustFlatten(t, `{"@timestamp":"t","log.level":"info","message":"m","http":{"request":{"method":"GET"},"response":{"status_code":200}},"url":{"path":"/x"},"event":{"duration":1000000}}`), true)
	tmpl := p.HeuristicTemplate()

	if tmpl.TimestampPath != "@timestamp" {
		t.Errorf("timestamp = %q", tmpl.TimestampPath)
	}
	if tmpl.LevelPath != "log.level" {
		t.Errorf("level = %q", tmpl.LevelPath)
	}
	if tmpl.MessagePath != "message" {
		t.Errorf("message = %q", tmpl.MessagePath)
	}
	if !hasHighlight(tmpl, "http.request.method") || !hasHighlight(tmpl, "http.response.status_code") {
		t.Errorf("expected method + status highlights, got %+v", tmpl.Highlights)
	}
}

func TestHeuristicTemplateNovelSchema(t *testing.T) {
	p := newProfile("sig")
	p.Observe(mustFlatten(t, `{"time":"t","severity":"WARN","msg":"hi","user":"bob"}`), true)
	tmpl := p.HeuristicTemplate()

	if tmpl.TimestampPath != "time" || tmpl.LevelPath != "severity" || tmpl.MessagePath != "msg" {
		t.Errorf("dynamic resolution failed: %+v", tmpl)
	}
	if !hasHighlight(tmpl, "user") {
		t.Errorf("expected 'user' highlight, got %+v", tmpl.Highlights)
	}
}

func TestHeuristicCacheInvalidates(t *testing.T) {
	p := newProfile("sig")
	p.Observe(mustFlatten(t, `{"level":"info"}`), true)
	first := p.HeuristicTemplate()
	if p.HeuristicTemplate() != first {
		t.Error("cache should return same pointer when unchanged")
	}
	p.Observe(mustFlatten(t, `{"level":"info","message":"m"}`), true)
	if p.HeuristicTemplate() == first {
		t.Error("cache should rebuild after a new field appears")
	}
}

func TestRender(t *testing.T) {
	color.NoColor = true
	p := newProfile("sig")
	flat := mustFlatten(t, `{"@timestamp":"2025-06-28T11:50:00.000Z","log.level":"error","message":"boom","http":{"request":{"method":"GET"},"response":{"status_code":500}},"url":{"path":"/api/x"},"event":{"duration":1250000000}}`)
	p.Observe(flat, true)

	var sb strings.Builder
	render(&sb, flat, p.HeuristicTemplate())
	out := sb.String()

	for _, want := range []string{"11:50:00", "erro", "boom", "GET", "500", "/api/x", "1250ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q; got: %s", want, out)
		}
	}
}

func TestFiltersAnchored(t *testing.T) {
	tmpl := &Template{LevelPath: "level", MessagePath: "message"}
	flat := map[string]interface{}{"level": "info", "message": "server started"}

	tests := []struct {
		name                            string
		level, message, noLvl, noMsg    string
		want                            bool
	}{
		{"level exact match", "info", "", "", "", true},
		{"level alternation", "error|info", "", "", "", true},
		{"level partial rejected", "inf", "", "", "", false},
		{"no-level excludes", "", "", "info", "", false},
		{"message regex match", "", "server.*", "", "", true},
		{"no-message excludes", "", "", "", "server.*", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := passesFilters(flat, tmpl,
				compile(tc.level), compile(tc.message), compile(tc.noLvl), compile(tc.noMsg))
			if got != tc.want {
				t.Errorf("passesFilters = %v, want %v", got, tc.want)
			}
		})
	}
}

func compile(p string) *regexp.Regexp {
	if p == "" {
		return nil
	}
	return regexp.MustCompile("^(?:" + p + ")$")
}

func hasHighlight(t *Template, path string) bool {
	for _, h := range t.Highlights {
		if h.Path == path {
			return true
		}
	}
	return false
}

func TestGetLevelColor(t *testing.T) {
	for _, level := range []string{"error", "warn", "info", "debug", "unknown"} {
		if getLevelColor(level) == nil {
			t.Errorf("nil color for %q", level)
		}
	}
}

func TestGetStatusColor(t *testing.T) {
	for _, status := range []int{200, 301, 404, 500, 0} {
		if getStatusColor(status) == nil {
			t.Errorf("nil color for %d", status)
		}
	}
}

func TestPrintHelp(t *testing.T) {
	out := captureStdout(t, printHelp)
	for _, want := range []string{
		"LogPipe - Pretty-print structured JSON logs",
		"USAGE:",
		"logpipe [OPTIONS]",
		"--llm",
		"EXAMPLES:",
		"github.com/kabooboo/logpipe",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestPrintVersion(t *testing.T) {
	out := captureStdout(t, printVersion)
	for _, want := range []string{"LogPipe", "Commit:", "Built:", "github.com/kabooboo/logpipe"} {
		if !strings.Contains(out, want) {
			t.Errorf("version missing %q", want)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func BenchmarkRender(b *testing.B) {
	color.NoColor = true
	var m map[string]interface{}
	json.Unmarshal([]byte(`{"@timestamp":"2025-06-28T11:50:00.000Z","log.level":"info","message":"access","http":{"request":{"method":"GET"},"response":{"status_code":200}},"url":{"path":"/api/test"},"event":{"duration":1000000}}`), &m)
	flat := flatten(m)
	p := newProfile("sig")
	p.Observe(flat, true)
	tmpl := p.HeuristicTemplate()
	var sb strings.Builder
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb.Reset()
		render(&sb, flat, tmpl)
	}
}
