package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/marcoantonios1/costguard/internal/config"
)

type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func NewSMTPSender(cfg config.EmailConfig) *SMTPSender {
	return &SMTPSender{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
	}
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return fmt.Errorf("no recipients provided")
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	auth := smtp.PlainAuth("", s.username, s.password, s.host)

	body := buildPlainEmail(s.from, msg.To, msg.Subject, msg.Text)

	return smtp.SendMail(addr, auth, s.from, msg.To, []byte(body))
}

func buildPlainEmail(from string, to []string, subject, text string) string {
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", strings.Join(to, ", ")),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		`Content-Type: text/plain; charset="UTF-8"`,
		"",
		text,
	}

	return strings.Join(headers, "\r\n")
}
