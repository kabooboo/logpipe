package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	reqChanBuffer  = 8
	llmTimeout     = 15 * time.Second
	refineCooldown = 50 // lines between refinements of the same cluster
)

// Store caches per-cluster templates and dedupes in-flight LLM requests.
// Get is called from the (single) render loop; set is called from the worker.
type Store struct {
	mu        sync.RWMutex
	templates map[string]*Template
	inflight  map[string]bool
	reqCh     chan ProfileSnapshot
	client    *llmClient
	debug     bool
	warned    bool
}

func newStore(client *llmClient, debug bool) *Store {
	return &Store{
		templates: make(map[string]*Template),
		inflight:  make(map[string]bool),
		reqCh:     make(chan ProfileSnapshot, reqChanBuffer),
		client:    client,
		debug:     debug,
	}
}

func (s *Store) Get(sig string) *Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates[sig]
}

func (s *Store) set(sig string, t *Template) {
	s.mu.Lock()
	s.templates[sig] = t
	s.inflight[sig] = false
	s.mu.Unlock()
}

// Enqueue schedules an LLM refinement for a cluster. Non-blocking: drops the
// request (returns false) if one is already in-flight for the signature or the
// queue is full.
func (s *Store) Enqueue(snap ProfileSnapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[snap.Signature] {
		return false
	}
	select {
	case s.reqCh <- snap:
		s.inflight[snap.Signature] = true
		return true
	default:
		return false
	}
}

func (s *Store) run(ctx context.Context) {
	for snap := range s.reqCh {
		if s.debug {
			fmt.Fprintf(stderr, "logpipe: LLM refine requested [%s] after %d samples\n", snap.Signature, snap.Count)
		}
		t, err := s.client.generate(ctx, snap)
		var reason string
		switch {
		case err != nil:
			reason = err.Error()
		case t == nil:
			reason = "empty response"
		case !t.validate(snap):
			reason = "template referenced unknown field paths"
		}
		if reason != "" {
			if s.debug {
				fmt.Fprintf(stderr, "logpipe: LLM refine rejected [%s]: %s\n", snap.Signature, reason)
			} else {
				s.warnOnce(fmt.Sprintf("logpipe: LLM schema generation failed: %s (using heuristic)\n", reason))
			}
			s.mu.Lock()
			s.inflight[snap.Signature] = false
			s.mu.Unlock()
			continue
		}
		if s.debug {
			fmt.Fprintf(stderr, "logpipe: LLM refine applied [%s] (%d highlights)\n", snap.Signature, len(t.Highlights))
		}
		t.Signature = snap.Signature
		t.source = "llm"
		s.set(snap.Signature, t)
	}
}

func (s *Store) warnOnce(msg string) {
	s.mu.Lock()
	already := s.warned
	s.warned = true
	s.mu.Unlock()
	if !already {
		fmt.Fprint(stderr, msg)
	}
}

type llmConfig struct {
	baseURL string
	apiKey  string
	model   string
}

type llmClient struct {
	cfg  llmConfig
	http *http.Client
}

func newLLMClient(cfg llmConfig) *llmClient {
	return &llmClient{cfg: cfg, http: &http.Client{Timeout: llmTimeout}}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	Temperature    float64        `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

const systemPrompt = `You configure a terminal log pretty-printer. Given the observed structure of one JSON log format (a list of dot-notation field paths with their types and sample values), choose how to render it.

Return ONLY a JSON object with this shape:
{
  "timestamp_path": "<path or empty>",
  "level_path": "<path or empty>",
  "message_path": "<path or empty>",
  "highlights": [ { "path": "<path>", "label": "<short label or empty>", "color": "<color>", "format": "<format>" } ]
}

Rules:
- Every path MUST be one of the provided field paths, verbatim. Never invent paths.
- timestamp_path: the primary time field. level_path: the severity/log-level field. message_path: the human-readable message.
- highlights: up to 6 additional fields worth surfacing (ids, status, method, path, duration, ip...). Order by usefulness. Omit noisy or redundant fields. Do not repeat the timestamp/level/message paths.
- color is one of: red, green, yellow, blue, magenta, cyan, white, gray, optionally "+bold" (e.g. "red+bold"), or "status" for HTTP status codes.
- format is one of: "" (plain), "ms-from-ns" (nanoseconds->ms), "bytes", "status", "truncate:N" (cap string length to N).
- label "" prints the bare value; otherwise it prints label=value.`

func (c *llmClient) generate(ctx context.Context, snap ProfileSnapshot) (*Template, error) {
	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	body := chatRequest{
		Model: c.cfg.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: describeProfile(snap)},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
		Temperature:    0,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.cfg.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, err
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("api error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	var t Template
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &t); err != nil {
		return nil, fmt.Errorf("bad template json: %w", err)
	}
	return &t, nil
}

// describeProfile renders the observed structure into the user prompt.
func describeProfile(snap ProfileSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Log format with %d samples. Fields:\n", snap.Count)
	for _, path := range snap.paths() {
		fs := snap.Fields[path]
		fmt.Fprintf(&b, "- %s (%s)", path, strings.Join(typeList(fs.Types), "|"))
		if len(fs.Samples) > 0 {
			fmt.Fprintf(&b, " e.g. %s", strings.Join(fs.Samples, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func typeList(types map[string]int) []string {
	ts := make([]string, 0, len(types))
	for t := range types {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	return ts
}
