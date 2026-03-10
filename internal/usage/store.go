package usage

import (
	"context"
	"time"
)

type Store interface {
	Save(ctx context.Context, record Record) error
	GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error)
	GetSpendByTeam(ctx context.Context, from, to time.Time) ([]TeamSpend, error)
}
