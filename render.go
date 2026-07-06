package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

// render prints one flattened log record using the given template.
func render(out *strings.Builder, flat map[string]interface{}, t *Template) {
	ts := time.Now()
	if raw := resolveString(flat, t.TimestampPath); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			ts = parsed
		}
	}
	timestampColor := color.New(color.FgCyan)
	out.WriteString(timestampColor.Sprint(ts.Format("15:04:05.000")))

	level := resolveString(flat, t.LevelPath)
	if level != "" {
		lc := getLevelColor(level)
		out.WriteString(" [")
		out.WriteString(lc.Sprintf("%-4s", level[:min(4, len(level))]))
		out.WriteString("]")
	}

	if msg := resolveString(flat, t.MessagePath); msg != "" {
		out.WriteString(" ")
		out.WriteString(color.New(color.FgWhite).Sprint(msg))
	}

	for _, h := range t.Highlights {
		v, ok := fieldValue(flat, h.Path)
		if !ok || v == nil {
			continue
		}
		text := formatValue(v, h.Format)
		if text == "" {
			continue
		}
		c := highlightColor(h, v)
		out.WriteString(" ")
		if h.Label != "" {
			out.WriteString(c.Sprintf("%s=%s", h.Label, text))
		} else {
			out.WriteString(c.Sprint(text))
		}
	}
	out.WriteByte('\n')
}

func formatValue(v interface{}, format string) string {
	switch {
	case format == "ms-from-ns":
		if n, ok := toInt64(v); ok {
			return fmt.Sprintf("%dms", n/1_000_000)
		}
	case format == "bytes":
		if n, ok := toInt64(v); ok {
			return fmt.Sprintf("%dB", n)
		}
	case format == "status":
		if n, ok := toInt64(v); ok {
			return strconv.FormatInt(n, 10)
		}
	case strings.HasPrefix(format, "truncate:"):
		s := scalarString(v)
		if n, err := strconv.Atoi(format[len("truncate:"):]); err == nil && len(s) > n {
			return s[:n]
		}
		return s
	}
	return scalarString(v)
}

func highlightColor(h Highlight, v interface{}) *color.Color {
	if h.Color == "status" {
		if n, ok := toInt64(v); ok {
			return getStatusColor(int(n))
		}
		return color.New(color.FgWhite)
	}
	return colorFor(h.Color)
}

var colorNames = map[string]color.Attribute{
	"red":     color.FgRed,
	"green":   color.FgGreen,
	"yellow":  color.FgYellow,
	"blue":    color.FgBlue,
	"magenta": color.FgMagenta,
	"cyan":    color.FgCyan,
	"white":   color.FgWhite,
	"gray":    color.FgHiBlack,
	"grey":    color.FgHiBlack,
}

func colorFor(name string) *color.Color {
	attrs := []color.Attribute{}
	for _, part := range strings.Split(name, "+") {
		switch part {
		case "bold":
			attrs = append(attrs, color.Bold)
		default:
			if a, ok := colorNames[part]; ok {
				attrs = append(attrs, a)
			}
		}
	}
	if len(attrs) == 0 {
		attrs = append(attrs, color.FgWhite)
	}
	return color.New(attrs...)
}

func toInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	default:
		return 0, false
	}
}
