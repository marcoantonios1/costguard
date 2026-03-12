package alert

import (
	"context"
	"time"
)

type Store interface {
	WasSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) (bool, error)
	MarkSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) error
}
