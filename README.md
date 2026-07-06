# LogPipe

A command-line tool for pretty-printing structured JSON logs, designed to make log analysis easier and more readable.

## Features

- 🤖 **LLM-driven formatting** — no built-in schema; an OpenAI-compatible model learns how to render each log format from the lines it sees
- 🌊 **Raw until ready** — lines print raw until a format's schema arrives, then switch to a colorized layout mid-stream
- 💾 **Cached schemas** — learned layouts are cached under `$HOME/.cache/logpipe/`, so repeat runs render instantly from the first line
- 📝 **Works with any JSON logs** — ECS, GCP, Bunyan, or your own shape
- 🔧 **Kubernetes-friendly** - works seamlessly with `kubectl logs`

## Installation

### Using Go Install (Recommended)

```bash
go install github.com/kabooboo/logpipe@latest
```

This will install the `logpipe` binary to your `$GOPATH/bin` directory (usually `~/go/bin`). Make sure this directory is in your `$PATH`.

**Requirements**: Go 1.21 or later

### Download from Releases

Download the latest binary from the [releases page](https://github.com/kabooboo/logpipe/releases).

### Build from Source

```bash
git clone https://github.com/kabooboo/logpipe.git
cd logpipe
go build .
```

## Usage

### Basic Usage

```bash
# Get help
logpipe --help

# Get version
logpipe --version

# Pipe JSON logs directly
echo '{"@timestamp":"2025-06-28T11:50:00.000Z","log.level":"info","message":"Application started"}' | logpipe

# Use with files
cat app.log | logpipe
```

### Filtering Logs

```bash
# Filter by log level (include only)
cat app.log | logpipe --level "error|warn"

# Filter by message content (include only)
cat app.log | logpipe --message "database.*timeout"

# Exclude log levels
cat app.log | logpipe --no-level "info"

# Exclude message patterns
cat app.log | logpipe --no-message "debug.*"

# Combine filters
cat app.log | logpipe --level "error|warn" --no-message "deprecated"
```

**Note**: Regex patterns are fully anchored — `--level "info"` matches the level `info` exactly, not `information`. Use alternation (`error|warn`) or wildcards (`.*timeout.*`) for partial matches. Filters apply once a format's schema is known.

### LLM formatting (default)

LogPipe has **no built-in schema**. An OpenAI-compatible model learns how to render each log format from the lines it observes. It is on by default:

```bash
export LLM_API_KEY=sk-...
export LLM_API_URL=https://my-gateway/v1   # optional; defaults to OpenAI
export LLM_MODEL=gpt-4o-mini               # optional
kubectl logs -f my-pod | logpipe
```

How it works:

- Lines print **raw** until a format's schema is ready; then LogPipe switches to a colorized layout and prints `☸ Adapted format …`.
- Schema generation runs in the background — the stream is never blocked on the network. If the endpoint is unreachable, lines keep flowing raw and one warning is printed to stderr (use `--llm-debug` to see per-request detail).
- Learned schemas are cached under `$HOME/.cache/logpipe/`, keyed by the logpipe command. **Repeat runs render structured from the first line** and make no LLM call. Disable with `--no-cache`.
- `--no-llm` turns off the model entirely and passes every line through raw.

**Privacy**: `--redact` (on by default) masks values whose key looks secret (`password`, `token`, `api_key`, …) before they are printed, cached, or sent. Other field **values** are sent to the endpoint so it can choose a layout — make sure that is acceptable for your data (PII / secrets) and endpoint.

### Kubernetes Logs

```bash
# View live logs from a pod
kubectl logs -f my-pod | logpipe

# View recent logs
kubectl logs my-pod --tail=100 | logpipe

# With short alias
k logs my-pod | logpipe
```

## Log Format Support

LogPipe intelligently detects and formats different types of logs:

### HTTP Access Logs

For logs with `category: "http"` and HTTP request data:

```
11:50:07 [info ] POST 200 /api/users from=192.168.1.100 1250ms ua=curl/8.7.1
```

### Application Logs

For general application logs:

```
11:50:00 [info ] Application started successfully
11:50:05 [error] Database connection failed error=map[code:CONN_TIMEOUT details:Connection timeout after 30s]
```

### Unparseable Lines

Non-JSON lines are truncated to fit terminal width:

```
This is a very long plain text log line that doesn't parse as JSON and will be truncated to fit...
```

## How fields are chosen

LogPipe has **no built-in schema**. For each line it flattens the JSON to dot-paths
(`http.request.method`) and groups logs by their set of top-level keys. After a few
samples of a format, it sends that structure (paths, types, sample values) to the LLM,
which picks the timestamp / level / message fields and a handful of highlights, plus
colors and formats (e.g. nanoseconds → `ms`, HTTP status coloring). The resulting layout
is cached and reused. Formats it hasn't learned yet print raw.

## Color Coding

- **Timestamps**: Cyan
- **Log Levels**: 
  - `error`: Red (bold)
  - `warn`: Yellow (bold)
  - `info`: Blue
  - `debug`: White
- **HTTP Methods**: Magenta (bold)
- **Status Codes**:
  - 2xx: Green
  - 3xx: Yellow
  - 4xx/5xx: Red
- **Paths**: Green
- **Durations**: Yellow
- **User Agents**: Blue
- **Error Details**: Red (bold)

## Examples

### Kubernetes Application Logs

```bash
$ kubectl logs my-app-pod | ./logpipe
11:45:32 [info ] GET  200 /health from=10.0.1.50 45ms ua=kube-probe/1.31+
11:45:37 [info ] POST 201 /api/users from=203.0.113.42 1200ms ua=curl/8.7.1
11:45:40 [error] Database query failed error=map[code:QUERY_TIMEOUT query:SELECT * FROM users]
```

### Mixed Log Types

```bash
$ cat mixed.log | ./logpipe
11:50:00 [info ] Application startup complete
11:50:05 [info ] GET  200 /api/status from=192.168.1.100 25ms ua=health-checker/1.0
11:50:10 [warn ] Rate limit approaching threshold=80%
11:50:15 [error] External service unavailable error=map[service:payment-api status:503]
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Changelog

See [RELEASES](https://github.com/kabooboo/logpipe/releases) for version history and changes.