package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
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
		fmt.Printf("%s [%s] %s %s %s %s %s %s\n",
			timestampColor.Sprintf(timestamp.Format("15:04:05")),
			levelColor.Sprintf("%-5s", log.Level),
			methodColor.Sprintf("%-4s", log.HTTP.Request.Method),
			statusColor.Sprintf("%d", log.HTTP.Response.StatusCode),
			pathColor.Sprintf("%s", log.URL.Path),
			color.New(color.FgWhite).Sprintf("from=%s", log.Source.IP),
			durationColor.Sprintf("%dms", log.Event.Duration/1000), // Convert to milliseconds
			color.New(color.FgBlue).Sprintf("ua=%s", log.UserAgent.Original),
		)
	} else {
		// Format general log entry
		fmt.Printf("%s [%s] %s",
			timestampColor.Sprintf(timestamp.Format("15:04:05")),
			levelColor.Sprintf("%-5s", log.Level),
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
	switch level {
	case "error":
		return color.New(color.FgRed, color.Bold)
	case "warn":
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
