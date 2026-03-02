package logging

import (
	"encoding/json"
	"log"
	"os"
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
		level: cfg.Level,
		std:   log.New(os.Stdout, "", 0),
	}
}

func (l *Log) Info(msg string, fields map[string]any) {
	l.write("info", msg, fields)
}

func (l *Log) Error(msg string, fields map[string]any) {
	l.write("error", msg, fields)
}

func (l *Log) write(level, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	fields["level"] = level
	fields["msg"] = msg

	if l.json {
		b, _ := json.Marshal(fields)
		l.std.Println(string(b))
		return
	}
	l.std.Printf("%s %s %v", level, msg, fields)
}