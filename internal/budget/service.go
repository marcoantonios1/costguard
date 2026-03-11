package budget

import (
	"context"
	"errors"
	"time"
)

var ErrMonthlyBudgetExceeded = errors.New("monthly budget exceeded")

type UsageReader interface {
	GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error)
}

type Config struct {
	Enabled    bool
	MonthlyUSD float64
}

type Service struct {
	usage UsageReader
	cfg   Config
}

func NewService(usage UsageReader, cfg Config) *Service {
	return &Service{
		usage: usage,
		cfg:   cfg,
	}
}

func (s *Service) CheckMonthlyBudget(ctx context.Context, now time.Time) error {
	if s == nil || !s.cfg.Enabled || s.cfg.MonthlyUSD <= 0 {
		return nil
	}

	from := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	total, err := s.usage.GetTotalSpend(ctx, from, to)
	if err != nil {
		return err
	}

	if total >= s.cfg.MonthlyUSD {
		return ErrMonthlyBudgetExceeded
	}

	return nil
}
