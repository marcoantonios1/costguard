package notify

import (
	"context"

	"github.com/marcoantonios1/costguard/internal/logging"
)

type LogSender struct {
	log *logging.Log
}

func NewLogSender(log *logging.Log) *LogSender {
	return &LogSender{log: log}
}

func (s *LogSender) Send(ctx context.Context, msg Message) error {
	if s.log != nil {
		s.log.Info("notification_sent", map[string]any{
			"channel": "log",
			"to":      msg.To,
			"subject": msg.Subject,
		})
	}
	return nil
}
