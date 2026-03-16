package budget

import (
	"context"
	"errors"
	"time"
)

var (
	ErrMonthlyBudgetExceeded             = errors.New("monthly budget exceeded")
	ErrMonthlyBudgetReachedEightyPercent = errors.New("monthly budget reached 80%")
	ErrMonthlyBudgetReachedNinetyPercent = errors.New("monthly budget reached 90%")
	ErrTeamBudgetExceeded                = errors.New("team monthly budget exceeded")
	ErrProjectBudgetExceeded             = errors.New("project monthly budget exceeded")
)

type UsageReader interface {
	GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error)
	GetSpendForTeam(ctx context.Context, team string, from, to time.Time) (float64, error)
	GetSpendForProject(ctx context.Context, project string, from, to time.Time) (float64, error)
}

type Config struct {
	Enabled    bool
	MonthlyUSD float64
	Teams      map[string]float64
	Projects   map[string]float64
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

func (s *Service) GetMonthlyStatus(ctx context.Context, now time.Time) (Status, error) {
	if s == nil {
		return Status{}, nil
	}

	from := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	total, err := s.usage.GetTotalSpend(ctx, from, to)
	if err != nil {
		return Status{}, err
	}

	percentage := 0.0
	if s.cfg.MonthlyUSD > 0 {
		percentage = (total / s.cfg.MonthlyUSD) * 100
	}

	remaining := s.cfg.MonthlyUSD - total
	if remaining < 0 {
		remaining = 0
	}

	return Status{
		PeriodStart:        from,
		PeriodEnd:          to,
		MonthlyBudgetUSD:   s.cfg.MonthlyUSD,
		CurrentSpendUSD:    total,
		PercentageUsed:     percentage,
		RemainingBudgetUSD: remaining,
		Exceeded:           total >= s.cfg.MonthlyUSD && s.cfg.MonthlyUSD > 0,
	}, nil
}

func (s *Service) CheckRequestBudget(
	ctx context.Context,
	now time.Time,
	team string,
	project string,
) error {

	if s == nil || !s.cfg.Enabled {
		return nil
	}

	from := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	// -------- GLOBAL BUDGET --------
	total, err := s.usage.GetTotalSpend(ctx, from, to)
	if err != nil {
		return err
	}

	if s.cfg.MonthlyUSD > 0 && total >= s.cfg.MonthlyUSD {
		return ErrMonthlyBudgetExceeded
	}

	// -------- TEAM BUDGET --------
	if team != "" {
		if limit, ok := s.cfg.Teams[team]; ok {

			spend, err := s.usage.GetSpendForTeam(ctx, team, from, to)
			if err != nil {
				return err
			}

			if limit > 0 && spend >= limit {
				return ErrTeamBudgetExceeded
			}
		}
	}

	// -------- PROJECT BUDGET --------
	if project != "" {
		if limit, ok := s.cfg.Projects[project]; ok {

			spend, err := s.usage.GetSpendForProject(ctx, project, from, to)
			if err != nil {
				return err
			}

			if limit > 0 && spend >= limit {
				return ErrProjectBudgetExceeded
			}
		}
	}

	return nil
}
