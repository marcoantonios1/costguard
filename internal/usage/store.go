package usage

import (
	"context"
	"time"
)

type Store interface {
	Save(ctx context.Context, record Record) error
	GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error)
	GetSpendByTeam(ctx context.Context, from, to time.Time) ([]TeamSpend, error)
	GetSpendByProvider(ctx context.Context, from, to time.Time) ([]ProviderSpend, error)
	GetSpendByModel(ctx context.Context, from, to time.Time) ([]ModelSpend, error)
	GetSpendByProject(ctx context.Context, from, to time.Time) ([]ProjectSpend, error)
	GetSpendForTeam(ctx context.Context, team string, from, to time.Time) (float64, error)
	GetSpendForProject(ctx context.Context, project string, from, to time.Time) (float64, error)
	GetSpendByAgent(ctx context.Context, from, to time.Time) ([]AgentSpend, error)
}
