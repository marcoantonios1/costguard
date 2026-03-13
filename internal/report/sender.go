package report

import (
	"context"
	"fmt"
	"time"

	"github.com/marcoantonios1/costguard/internal/notify"
)

type Notifier interface {
	Send(ctx context.Context, msg notify.Message) error
}

type EmailService struct {
	reports   *Service
	notifier  Notifier
	deliveries DeliveryStore
}

func NewEmailService(reports *Service, notifier Notifier, deliveries DeliveryStore) *EmailService {
	return &EmailService{
		reports:    reports,
		notifier:   notifier,
		deliveries: deliveries,
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

func (s *EmailService) SendMonthlyUsageReportIfNotSent(ctx context.Context, now time.Time) error {
	if s.reports == nil {
		return fmt.Errorf("report service not configured")
	}
	if s.notifier == nil {
		return fmt.Errorf("notifier not configured")
	}
	if s.deliveries == nil {
		return fmt.Errorf("delivery store not configured")
	}

	periodStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	reportType := "usage"

	sent, err := s.deliveries.WasMonthlyReportSent(ctx, periodStart, reportType)
	if err != nil {
		return err
	}
	if sent {
		return nil
	}

	if err := s.SendMonthlyUsageReport(ctx, now); err != nil {
		return err
	}

	return s.deliveries.MarkMonthlyReportSent(ctx, periodStart, reportType)
}