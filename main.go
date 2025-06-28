package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

// Version information - set at build time
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type LogEntry struct {
	Timestamp   string      `json:"@timestamp"`
	Level       string      `json:"log.level"`
	Message     string      `json:"message"`
	Category    string      `json:"category"`
	Error       interface{} `json:"error"`
	Destination struct {
		Domain string `json:"domain"`
	} `json:"destination"`
	Event struct {
		Duration int64 `json:"duration"`
	} `json:"event"`
	HTTP struct {
		Request struct {
			Body struct {
				Bytes int `json:"bytes"`
			} `json:"body"`
			ID     string `json:"id"`
			Method string `json:"method"`
			Time   string `json:"time"`
		} `json:"request"`
		Response struct {
			Body struct {
				Bytes int `json:"bytes"`
			} `json:"body"`
			MimeType   string `json:"mime_type"`
			StatusCode int    `json:"status_code"`
		} `json:"response"`
		Version string `json:"version"`
	} `json:"http"`
	Log struct {
		Logger   string `json:"logger"`
		Original string `json:"original"`
		Origin   struct {
			File struct {
				Line int    `json:"line"`
				Name string `json:"name"`
			} `json:"file"`
			Function string `json:"function"`
		} `json:"origin"`
	} `json:"log"`
	Process struct {
		Name   string `json:"name"`
		PID    int    `json:"pid"`
		Thread struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"thread"`
	} `json:"process"`
	Service struct {
		Version string `json:"version"`
	} `json:"service"`
	Source struct {
		IP string `json:"ip"`
	} `json:"source"`
	Span  interface{} `json:"span"`
	Trace interface{} `json:"trace"`
	URL   struct {
		Domain       string `json:"domain"`
		Path         string `json:"path"`
		PathTemplate string `json:"path_template"`
		Port         int    `json:"port"`
		Query        string `json:"query"`
		Scheme       string `json:"scheme"`
	} `json:"url"`
	UserAgent struct {
		Original string `json:"original"`
	} `json:"user_agent"`
	Version string `json:"version"`
}

func main() {
	// Check for help flags
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "-h" || arg == "--help" || arg == "help" {
			printHelp()
			return
		}
		if arg == "-v" || arg == "--version" || arg == "version" {
			printVersion()
			return
		}
	}

	// Check if stdin has data
	stat, err := os.Stdin.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking stdin: %v\n", err)
		os.Exit(1)
	}

	// If no pipe input and no args, show help
	if (stat.Mode()&os.ModeCharDevice) != 0 && len(os.Args) == 1 {
		printHelp()
		return
	}

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Text()

		var logEntry LogEntry
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			// If not valid JSON, print the line truncated to fit terminal
			if len(line) > 120 {
				fmt.Printf("%s...\n", line[:120])
			} else {
				fmt.Println(line)
			}
			continue
		}

		printPrettyLog(logEntry)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
		os.Exit(1)
	}
}

func printPrettyLog(log LogEntry) {
	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, log.Timestamp)
	if err != nil {
		timestamp = time.Now()
	}

	// Color setup
	timestampColor := color.New(color.FgCyan)
	levelColor := getLevelColor(log.Level)
	methodColor := color.New(color.FgMagenta, color.Bold)
	statusColor := getStatusColor(log.HTTP.Response.StatusCode)
	durationColor := color.New(color.FgYellow)
	pathColor := color.New(color.FgGreen)
	messageColor := color.New(color.FgWhite)

	// Check if this is an HTTP access log
	if log.Category == "http" && log.HTTP.Request.Method != "" {
		// Format HTTP access log
		userAgent := log.UserAgent.Original
		if len(userAgent) > 50 {
			userAgent = userAgent[:50]
		}
		fmt.Printf("%s [%s] %s %s %s %s %s %s\n",
			timestampColor.Sprintf(timestamp.Format("15:04:05.000")),
			levelColor.Sprintf("%-4s", log.Level[:min(4, len(log.Level))]),
			methodColor.Sprintf("%-4s", log.HTTP.Request.Method),
			statusColor.Sprintf("%d", log.HTTP.Response.StatusCode),
			pathColor.Sprintf("%s", log.URL.Path),
			durationColor.Sprintf("%dms", log.Event.Duration/1000000), // Convert to milliseconds
			color.New(color.FgBlue).Sprintf("ua=%s", userAgent),
			messageColor.Sprintf("%s", log.Message),
		)
	} else {
		// Format general log entry
		fmt.Printf("%s [%s] %s",
			timestampColor.Sprintf(timestamp.Format("15:04:05.000")),
			levelColor.Sprintf("%-4s", log.Level[:min(4, len(log.Level))]),
			messageColor.Sprintf("%s", log.Message),
		)

		// Add error information if present
		if log.Error != nil {
			errorColor := color.New(color.FgRed, color.Bold)
			fmt.Printf(" %s", errorColor.Sprintf("error=%v", log.Error))
		}

		fmt.Println()
	}
}

func getLevelColor(level string) *color.Color {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return color.New(color.FgRed, color.Bold)
	case "warn", "warning":
		return color.New(color.FgYellow, color.Bold)
	case "info":
		return color.New(color.FgBlue)
	case "debug":
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
	fmt.Println("  It automatically detects HTTP access logs and general application logs.")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("  -h, --help     Show this help message")
	fmt.Println("  -v, --version  Show version information")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  # Kubernetes logs")
	fmt.Println("  kubectl logs my-pod | logpipe")
	fmt.Println()
	fmt.Println("  # Local log files")
	fmt.Println("  cat app.log | logpipe")
	fmt.Println()
	fmt.Println("  # Live log streaming")
	fmt.Println("  tail -f /var/log/app.log | logpipe")
	fmt.Println()
	fmt.Println("  # JSON log example")
	fmt.Println(`  echo '{"@timestamp":"2024-01-15T14:25:13.458Z","log.level":"info","message":"Server started"}' | logpipe`)
	fmt.Println()
	fmt.Println("OUTPUT FORMATS:")
	fmt.Println("  HTTP Access Logs:")
	fmt.Println("    14:25:13 [info ] GET  200 /api/users from=192.168.1.100 850ms ua=curl/8.7.1")
	fmt.Println()
	fmt.Println("  Application Logs:")
	fmt.Println("    14:25:13 [error] Database connection failed error=map[code:TIMEOUT]")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/kabooboo/logpipe")
}

func printVersion() {
	fmt.Printf("LogPipe %s\n", version)
	fmt.Printf("Commit: %s\n", commit)
	fmt.Printf("Built: %s\n", date)
	fmt.Println("https://github.com/kabooboo/logpipe")
}
