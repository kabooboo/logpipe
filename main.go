package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// Version information - set at build time
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var stderr io.Writer = os.Stderr

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" || arg == "help" {
			printHelp()
			return
		}
		if arg == "-v" || arg == "--version" || arg == "version" {
			printVersion()
			return
		}
	}

	levelFilter := flag.String("level", "", "PERL regex to filter log levels")
	messageFilter := flag.String("message", "", "PERL regex to filter messages")
	noLevelFilter := flag.String("no-level", "", "PERL regex to exclude log levels")
	noMessageFilter := flag.String("no-message", "", "PERL regex to exclude messages")
	noLLM := flag.Bool("no-llm", false, "disable LLM formatting; pass raw lines through unchanged")
	llmDebug := flag.Bool("llm-debug", false, "log LLM refinement activity to stderr")
	llmModel := flag.String("llm-model", envOr("LLM_MODEL", "gpt-4o-mini"), "LLM model name")
	redact := flag.Bool("redact", true, "mask secret-looking values before sampling/printing/sending")
	minSamples := flag.Int("llm-min-samples", 5, "lines to observe before the first LLM refinement")
	noCache := flag.Bool("no-cache", false, "do not read or write the on-disk schema cache")
	flag.Parse()

	levelRegex := mustCompile(*levelFilter, "level")
	messageRegex := mustCompile(*messageFilter, "message")
	noLevelRegex := mustCompile(*noLevelFilter, "no-level")
	noMessageRegex := mustCompile(*noMessageFilter, "no-message")

	stat, err := os.Stdin.Stat()
	if err != nil {
		fmt.Fprintf(stderr, "Error checking stdin: %v\n", err)
		os.Exit(1)
	}
	if (stat.Mode()&os.ModeCharDevice) != 0 && len(os.Args) == 1 {
		printHelp()
		return
	}

	profiles := newProfiles(*redact)

	var store *Store
	if !*noLLM {
		cfg := llmConfig{
			baseURL: envOr("LLM_API_URL", "https://api.openai.com/v1"),
			apiKey:  os.Getenv("LLM_API_KEY"),
			model:   *llmModel,
		}
		cacheFile := ""
		if !*noCache {
			cacheFile = cachePath(commandChecksum(os.Args[1:]))
		}
		store = newStore(newLLMClient(cfg), *llmDebug, cacheFile)
		go store.run(context.Background())
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var sb strings.Builder
	seen := make(map[string]*Template)
	if store != nil {
		for _, sig := range store.cachedSignatures() {
			seen[sig] = store.Get(sig)
		}
	}
	cueColor := color.New(color.FgHiBlack, color.Italic)

	for scanner.Scan() {
		line := scanner.Text()

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			if len(line) > 120 {
				fmt.Fprintf(out, "%s...\n", line[:120])
			} else {
				fmt.Fprintln(out, line)
			}
			continue
		}

		flat := flatten(raw)
		redactFlat(flat, *redact)
		sig := signature(flat)
		profile := profiles.route(sig, flat)
		maybeRefine(store, profile, *minSamples)

		var tmpl *Template
		if store != nil {
			tmpl = store.Get(sig)
		}
		if tmpl == nil {
			fmt.Fprintln(out, line)
			continue
		}

		if seen[sig] != tmpl {
			fmt.Fprintln(out, cueColor.Sprintf("☸ Adapted format · %d fields", len(profile.Fields)))
		}
		seen[sig] = tmpl

		if !passesFilters(flat, tmpl, levelRegex, messageRegex, noLevelRegex, noMessageRegex) {
			continue
		}

		sb.Reset()
		render(&sb, flat, tmpl)
		out.WriteString(sb.String())
	}

	if err := scanner.Err(); err != nil {
		out.Flush()
		fmt.Fprintf(stderr, "Error reading from stdin: %v\n", err)
		os.Exit(1)
	}
}

// maybeRefine enqueues an LLM refinement when enough has been seen, or when new
// fields have appeared and the cooldown has elapsed.
func maybeRefine(store *Store, profile *Profile, minSamples int) {
	if store == nil || profile.Count < minSamples {
		return
	}
	// A cached/existing template already covers this format; anchor the gen
	// markers to it so we only re-query when genuinely new fields appear.
	if profile.lastGenCount == 0 && store.Get(profile.Signature) != nil {
		profile.lastGenPaths = len(profile.Fields)
		profile.lastGenCount = profile.Count
		return
	}
	firstGen := profile.lastGenCount == 0
	newFields := len(profile.Fields) > profile.lastGenPaths
	cooled := profile.Count-profile.lastGenCount >= refineCooldown
	if !firstGen && !(newFields && cooled) {
		return
	}
	if store.Enqueue(profile.Snapshot()) {
		profile.lastGenPaths = len(profile.Fields)
		profile.lastGenCount = profile.Count
	}
}

func passesFilters(flat map[string]interface{}, tmpl *Template, level, message, noLevel, noMessage *regexp.Regexp) bool {
	levelVal := resolveString(flat, tmpl.LevelPath)
	messageVal := resolveString(flat, tmpl.MessagePath)
	if level != nil && !level.MatchString(levelVal) {
		return false
	}
	if message != nil && !message.MatchString(messageVal) {
		return false
	}
	if noLevel != nil && noLevel.MatchString(levelVal) {
		return false
	}
	if noMessage != nil && noMessage.MatchString(messageVal) {
		return false
	}
	return true
}

// mustCompile compiles an anchored filter pattern, exiting on error.
func mustCompile(pattern, name string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		fmt.Fprintf(stderr, "Invalid %s regex: %v\n", name, err)
		os.Exit(1)
	}
	return re
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getLevelColor(level string) *color.Color {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error", "err", "fatal", "critical":
		return color.New(color.FgRed, color.Bold)
	case "warn", "warning":
		return color.New(color.FgYellow, color.Bold)
	case "info":
		return color.New(color.FgBlue)
	case "debug", "trace":
		return color.New(color.FgWhite)
	default:
		return color.New(color.FgWhite)
	}
}

func getStatusColor(status int) *color.Color {
	switch {
	case status >= 200 && status < 300:
		return color.New(color.FgGreen)
	case status >= 300 && status < 400:
		return color.New(color.FgYellow)
	case status >= 400 && status < 500:
		return color.New(color.FgRed)
	case status >= 500:
		return color.New(color.FgRed, color.Bold)
	default:
		return color.New(color.FgWhite)
	}
}

func printHelp() {
	fmt.Println("LogPipe - Pretty-print structured JSON logs")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  logpipe [OPTIONS]")
	fmt.Println()
	fmt.Println("DESCRIPTION:")
	fmt.Println("  LogPipe reads JSON logs from stdin and displays them in a readable format.")
	fmt.Println("  It has NO built-in schema: an OpenAI-compatible LLM learns how to render each")
	fmt.Println("  log format from the lines it sees. Until a format's schema is ready, lines are")
	fmt.Println("  printed raw; learned schemas are cached so later runs render instantly.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -h, --help              Show this help message")
	fmt.Println("  -v, --version           Show version information")
	fmt.Println("  --level REGEX           Include logs matching level regex")
	fmt.Println("  --message REGEX         Include logs matching message regex")
	fmt.Println("  --no-level REGEX        Exclude logs matching level regex")
	fmt.Println("  --no-message REGEX      Exclude logs matching message regex")
	fmt.Println("  --no-llm                Disable the LLM; pass raw lines through unchanged")
	fmt.Println("  --llm-debug             Log LLM refinement activity to stderr")
	fmt.Println("  --llm-model NAME        Model name (default: $LLM_MODEL or gpt-4o-mini)")
	fmt.Println("  --llm-min-samples N     Lines to observe before first refinement (default: 5)")
	fmt.Println("  --no-cache              Do not read or write the on-disk schema cache")
	fmt.Println("  --redact                Mask secret-looking values (default: true)")
	fmt.Println()
	fmt.Println("LLM ENVIRONMENT:")
	fmt.Println("  LLM_API_URL             API base (default: https://api.openai.com/v1)")
	fmt.Println("  LLM_API_KEY             API key sent as a Bearer token")
	fmt.Println("  LLM_MODEL               Default model name")
	fmt.Println()
	fmt.Println("  Schemas are cached under $HOME/.cache/logpipe/ keyed by the logpipe command.")
	fmt.Println("  Note: unless --redact=false, secret-looking values are masked before being")
	fmt.Println("  sent; other field VALUES are sent to the endpoint so it can pick a layout.")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  kubectl logs -f my-pod | logpipe")
	fmt.Println("  cat app.log | logpipe --level \"error|warn\"")
	fmt.Println("  cat app.log | logpipe --no-llm")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/kabooboo/logpipe")
}

func printVersion() {
	fmt.Printf("LogPipe %s\n", version)
	fmt.Printf("Commit: %s\n", commit)
	fmt.Printf("Built: %s\n", date)
	fmt.Println("https://github.com/kabooboo/logpipe")
}
