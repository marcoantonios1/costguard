package alert

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

func (s *PostgresStore) WasSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) (bool, error) {
	var exists bool

	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM budget_alerts
			WHERE period_start = $1
			  AND threshold_percent = $2
			  AND alert_type = $3
		)
	`, periodStart, thresholdPercent, alertType).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (s *PostgresStore) MarkSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO budget_alerts (period_start, threshold_percent, alert_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (period_start, threshold_percent, alert_type) DO NOTHING
	`, periodStart, thresholdPercent, alertType)

	return err
}
