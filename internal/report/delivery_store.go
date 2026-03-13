package report

import (
	"context"
	"time"
)

type DeliveryStore interface {
	WasMonthlyReportSent(ctx context.Context, periodStart time.Time, reportType string) (bool, error)
	MarkMonthlyReportSent(ctx context.Context, periodStart time.Time, reportType string) error
}
