package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mockChatServer(t *testing.T, templateJSON string, captured *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if captured != nil {
			*captured = string(body)
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": templateJSON}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestLLMGenerate(t *testing.T) {
	tmplJSON := `{"timestamp_path":"@timestamp","level_path":"log.level","message_path":"message","highlights":[{"path":"url.path","label":"","color":"green","format":""}]}`
	srv := mockChatServer(t, tmplJSON, nil)
	defer srv.Close()

	client := newLLMClient(llmConfig{baseURL: srv.URL, model: "test"})
	snap := ProfileSnapshot{
		Signature: "sig",
		Count:     10,
		Fields: map[string]FieldStat{
			"@timestamp": {Count: 10, Types: map[string]int{"string": 10}},
			"log.level":  {Count: 10, Types: map[string]int{"string": 10}},
			"message":    {Count: 10, Types: map[string]int{"string": 10}},
			"url.path":   {Count: 10, Types: map[string]int{"string": 10}},
		},
	}
	tmpl, err := client.generate(context.Background(), snap)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if tmpl.LevelPath != "log.level" || !tmpl.validate(snap) {
		t.Errorf("unexpected template: %+v", tmpl)
	}
	if !hasHighlight(tmpl, "url.path") {
		t.Errorf("missing highlight: %+v", tmpl.Highlights)
	}
}

func TestLLMRequestRedaction(t *testing.T) {
	var captured string
	srv := mockChatServer(t, `{"timestamp_path":"","level_path":"","message_path":"","highlights":[]}`, &captured)
	defer srv.Close()

	client := newLLMClient(llmConfig{baseURL: srv.URL, model: "test"})
	snap := ProfileSnapshot{
		Signature: "sig",
		Count:     5,
		Fields: map[string]FieldStat{
			"user.password": {Count: 5, Types: map[string]int{"string": 5}, Samples: []string{"***"}},
		},
	}
	if _, err := client.generate(context.Background(), snap); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(captured, "hunter2") {
		t.Error("real secret value leaked into LLM request")
	}
	if !strings.Contains(captured, "***") {
		t.Error("redacted sample should be present in request")
	}
}

func TestLLMValidateRejectsUnknownPaths(t *testing.T) {
	snap := ProfileSnapshot{Fields: map[string]FieldStat{"a": {}}}
	bad := &Template{TimestampPath: "nonexistent"}
	if bad.validate(snap) {
		t.Error("validate should reject a template referencing unknown paths")
	}
	dropHL := &Template{Highlights: []Highlight{{Path: "a"}, {Path: "ghost"}}}
	if !dropHL.validate(snap) {
		t.Error("validate should accept known core paths")
	}
	if len(dropHL.Highlights) != 1 || dropHL.Highlights[0].Path != "a" {
		t.Errorf("validate should drop unknown highlight paths, got %+v", dropHL.Highlights)
	}
}

func TestStoreEnqueueDedupe(t *testing.T) {
	store := newStore(nil, false)
	snap := ProfileSnapshot{Signature: "sig"}
	if !store.Enqueue(snap) {
		t.Fatal("first enqueue should succeed")
	}
	if store.Enqueue(snap) {
		t.Error("second enqueue for in-flight signature should be dropped")
	}
}
