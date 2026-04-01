package report

import (
	"context"
	"time"

	"github.com/marcoantonios1/costguard/internal/usage"
)

type UsageReader interface {
	GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error)
	GetSpendByTeam(ctx context.Context, from, to time.Time) ([]usage.TeamSpend, error)
	GetSpendByProvider(ctx context.Context, from, to time.Time) ([]usage.ProviderSpend, error)
	GetSpendByModel(ctx context.Context, from, to time.Time) ([]usage.ModelSpend, error)
}

type MonthlySummary struct {
	From         time.Time             `json:"from"`
	To           time.Time             `json:"to"`
	TotalSpend   float64               `json:"total_spend_usd"`
	ByTeam       []usage.TeamSpend     `json:"by_team"`
	ByProvider   []usage.ProviderSpend `json:"by_provider"`
	ByModel      []usage.ModelSpend    `json:"by_model"`
}

type Service struct {
	usage UsageReader
}

func NewService(usage UsageReader) *Service {
	return &Service{usage: usage}
}

func (s *Service) BuildMonthlySummary(ctx context.Context, now time.Time) (MonthlySummary, error) {
	to := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, -1, 0)

	total, err := s.usage.GetTotalSpend(ctx, from, to)
	if err != nil {
		return MonthlySummary{}, err
	}

	byTeam, err := s.usage.GetSpendByTeam(ctx, from, to)
	if err != nil {
		return MonthlySummary{}, err
	}

	byProvider, err := s.usage.GetSpendByProvider(ctx, from, to)
	if err != nil {
		return MonthlySummary{}, err
	}

	byModel, err := s.usage.GetSpendByModel(ctx, from, to)
	if err != nil {
		return MonthlySummary{}, err
	}

	return MonthlySummary{
		From:       from,
		To:         to,
		TotalSpend: total,
		ByTeam:     byTeam,
		ByProvider: byProvider,
		ByModel:    byModel,
	}, nil
}