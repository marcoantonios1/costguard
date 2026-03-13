package report

import (
	"context"
	"time"

	"github.com/marcoantonios1/costguard/internal/notify"
)

type Notifier interface {
	Send(ctx context.Context, msg notify.Message) error
}

type EmailService struct {
	reports  *Service
	notifier Notifier
}

func NewEmailService(reports *Service, notifier Notifier) *EmailService {
	return &EmailService{
		reports:  reports,
		notifier: notifier,
	}
}

func (s *EmailService) SendMonthlyUsageReport(ctx context.Context, now time.Time) error {
	summary, err := s.reports.BuildMonthlySummary(ctx, now)
	if err != nil {
		return err
	}

	subject, body := FormatMonthlySummaryEmail(summary)

	return s.notifier.Send(ctx, notify.Message{
		Subject: subject,
		Text:    body,
	})
}