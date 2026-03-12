package budget

import (
	"context"
	"errors"
	"time"
)

var ErrMonthlyBudgetExceeded = errors.New("monthly budget exceeded")
var ErrMonthlyBudgetReachedEightyPercent = errors.New("monthly budget reached 80%")
var ErrMonthlyBudgetReachedNinetyPercent = errors.New("monthly budget reached 90%")

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

func (s *Service) MonthlyWindow(now time.Time) (time.Time, time.Time) {
	from := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	return from, to
}

func (s *Service) CurrentMonthlySpend(ctx context.Context, now time.Time) (float64, time.Time, time.Time, error) {
	if s == nil {
		return 0, time.Time{}, time.Time{}, nil
	}

	from, to := s.MonthlyWindow(now)

	total, err := s.usage.GetTotalSpend(ctx, from, to)
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}

	return total, from, to, nil
}

func (s *Service) CheckMonthlyBudget(ctx context.Context, now time.Time) error {
	if s == nil || !s.cfg.Enabled || s.cfg.MonthlyUSD <= 0 {
		return nil
	}

	total, _, _, err := s.CurrentMonthlySpend(ctx, now)
	if err != nil {
		return err
	}

	if total >= s.cfg.MonthlyUSD {
		return ErrMonthlyBudgetExceeded
	}
	if total >= s.cfg.MonthlyUSD*0.9 {
		return ErrMonthlyBudgetReachedNinetyPercent
	}
	if total >= s.cfg.MonthlyUSD*0.8 {
		return ErrMonthlyBudgetReachedEightyPercent
	}

	return nil
}