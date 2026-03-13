package report

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDeliveryStore struct {
	db *pgxpool.Pool
}

func NewPostgresDeliveryStore(db *pgxpool.Pool) *PostgresDeliveryStore {
	return &PostgresDeliveryStore{db: db}
}

func (s *PostgresDeliveryStore) WasMonthlyReportSent(ctx context.Context, periodStart time.Time, reportType string) (bool, error) {
	var exists bool

	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM monthly_reports
			WHERE period_start = $1
			  AND report_type = $2
		)
	`, periodStart, reportType).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (s *PostgresDeliveryStore) MarkMonthlyReportSent(ctx context.Context, periodStart time.Time, reportType string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO monthly_reports (period_start, report_type)
		VALUES ($1, $2)
		ON CONFLICT (period_start, report_type) DO NOTHING
	`, periodStart, reportType)

	return err
}