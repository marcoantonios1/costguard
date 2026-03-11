package logging

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/config"
)

type Log struct {
	json  bool
	level string
	std   *log.Logger
}

func New(cfg config.LoggingConfig) *Log {
	return &Log{
		json:  cfg.JSON,
		level: strings.ToLower(cfg.Level),
		std:   log.New(os.Stdout, "", 0),
	}
}

func (l *Log) Info(msg string, fields map[string]any) {
	if !l.shouldLog("info") {
		return
	}
	l.write("info", msg, fields)
}

func (l *Log) Warn(msg string, fields map[string]any) {
	if !l.shouldLog("warn") {
		return
	}
	l.write("warn", msg, fields)
}

func (l *Log) Error(msg string, fields map[string]any) {
	if !l.shouldLog("error") {
		return
	}
	l.write("error", msg, fields)
}

func (l *Log) Debug(msg string, fields map[string]any) {
	if !l.shouldLog("debug") {
		return
	}
	l.write("debug", msg, fields)
}

func (l *Log) shouldLog(level string) bool {
	order := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}

	current, ok := order[l.level]
	if !ok {
		current = 1
	}

	return order[level] >= current
}

func (l *Log) write(level, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}

	ts := time.Now().Format("15:04:05")

	if l.json {
		fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
		fields["level"] = level
		fields["msg"] = msg
		b, _ := json.Marshal(fields)
		l.std.Println(string(b))
		return
	}

	color := levelColor(level)
	levelStr := strings.ToUpper(level)

	// Build field string nicely
	fieldStr := ""
	for k, v := range fields {
		fieldStr += fmt.Sprintf(" %s=%v", k, v)
	}

	l.std.Printf(
		"%s %s%-5s%s %s%s\n",
		dim(ts),
		color,
		levelStr,
		reset(),
		msg,
		fieldStr,
	)
}

/* ---------- COLORS ---------- */

func levelColor(level string) string {
	switch level {
	case "debug":
		return blue()
	case "info":
		return green()
	case "warn":
		return yellow()
	case "error":
		return red()
	default:
		return white()
	}
}

func reset() string { return "\033[0m" }
func red() string   { return "\033[31m" }
func green() string { return "\033[32m" }
func blue() string  { return "\033[34m" }
func yellow() string { return "\033[33m" }
func white() string { return "\033[37m" }
func dim(s string) string {
	return "\033[2m" + s + "\033[0m"
}
