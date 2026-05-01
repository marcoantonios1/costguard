package usage

import (
	"context"
	"time"

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
			agent,
			path,
			status_code,
			metering_estimated
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
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
		nullIfEmpty(r.Agent),
		r.Path,
		r.StatusCode,
		r.MeteringEstimated,
	)

	return err
}

func (s *PostgresStore) GetTotalSpend(ctx context.Context, from, to time.Time) (float64, error) {
	var total float64

	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
	`, from, to).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (s *PostgresStore) GetSpendByTeam(ctx context.Context, from, to time.Time) ([]TeamSpend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(team, ''), COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		GROUP BY team
		ORDER BY SUM(estimated_cost_usd) DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TeamSpend
	for rows.Next() {
		var item TeamSpend
		if err := rows.Scan(&item.Team, &item.Spend); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *PostgresStore) GetSpendByProvider(ctx context.Context, from, to time.Time) ([]ProviderSpend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(provider, ''), COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		GROUP BY provider
		ORDER BY SUM(estimated_cost_usd) DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProviderSpend
	for rows.Next() {
		var item ProviderSpend
		if err := rows.Scan(&item.Provider, &item.Spend); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *PostgresStore) GetSpendByModel(ctx context.Context, from, to time.Time) ([]ModelSpend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(model, ''), COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		GROUP BY model
		ORDER BY SUM(estimated_cost_usd) DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ModelSpend
	for rows.Next() {
		var item ModelSpend
		if err := rows.Scan(&item.Model, &item.Spend); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *PostgresStore) GetSpendByProject(ctx context.Context, from, to time.Time) ([]ProjectSpend, error) {
	query := `
	SELECT COALESCE(project, ''), COALESCE(SUM(estimated_cost_usd), 0)
	FROM usage_records
	WHERE timestamp_utc >= $1
	  AND timestamp_utc < $2
	GROUP BY project
	ORDER BY SUM(estimated_cost_usd) DESC
	`

	rows, err := s.db.Query(ctx, query, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProjectSpend

	for rows.Next() {
		var p ProjectSpend

		if err := rows.Scan(&p.Project, &p.Spend); err != nil {
			return nil, err
		}

		result = append(result, p)
	}

	return result, rows.Err()
}

func (s *PostgresStore) GetSpendForTeam(ctx context.Context, team string, from, to time.Time) (float64, error) {
	var total float64

	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		  AND team = $3
	`, from, to, team).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (s *PostgresStore) GetSpendForProject(ctx context.Context, project string, from, to time.Time) (float64, error) {
	var total float64

	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		  AND project = $3
	`, from, to, project).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (s *PostgresStore) GetSpendForAgent(ctx context.Context, agent string, from, to time.Time) (float64, error) {
	var total float64

	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		  AND agent = $3
	`, from, to, agent).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (s *PostgresStore) GetSpendByAgent(ctx context.Context, from, to time.Time) ([]AgentSpend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(agent, ''), COALESCE(SUM(estimated_cost_usd), 0)
		FROM usage_records
		WHERE timestamp_utc >= $1
		  AND timestamp_utc < $2
		GROUP BY agent
		ORDER BY SUM(estimated_cost_usd) DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AgentSpend
	for rows.Next() {
		var item AgentSpend
		if err := rows.Scan(&item.Agent, &item.Spend); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
