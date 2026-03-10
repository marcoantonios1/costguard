package usage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, r Record) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO usage_records (
			request_id,
			timestamp_utc,
			provider,
			model,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			estimated_cost_usd,
			price_found,
			cache_hit,
			team,
			project,
			user_name,
			path,
			status_code
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
		)
	`,
		r.RequestID,
		r.Timestamp,
		r.Provider,
		r.Model,
		r.PromptTokens,
		r.CompletionTokens,
		r.TotalTokens,
		r.EstimatedCostUSD,
		r.PriceFound,
		r.CacheHit,
		nullIfEmpty(r.Team),
		nullIfEmpty(r.Project),
		nullIfEmpty(r.User),
		r.Path,
		r.StatusCode,
	)

	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}