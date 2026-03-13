package report

import (
	"context"
	"time"

	"github.com/marcoantonios1/costguard/internal/logging"
)

type SchedulerConfig struct {
	Enabled       bool
	CheckInterval time.Duration
	RunOnStartup  bool
}

type Scheduler struct {
	email *EmailService
	log   *logging.Log
	cfg   SchedulerConfig
}

func NewScheduler(email *EmailService, log *logging.Log, cfg SchedulerConfig) *Scheduler {
	return &Scheduler{
		email: email,
		log:   log,
		cfg:   cfg,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || !s.cfg.Enabled || s.email == nil {
		return
	}

	interval := s.cfg.CheckInterval
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	if s.cfg.RunOnStartup {
		s.runOnce(ctx)
	}

	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				if s.log != nil {
					s.log.Info("monthly_report_scheduler_stopped", nil)
				}
				return

			case <-ticker.C:
				s.runOnce(ctx)
			}
		}
	}()

	if s.log != nil {
		s.log.Info("monthly_report_scheduler_started", map[string]any{
			"check_interval": interval.String(),
			"run_on_startup": s.cfg.RunOnStartup,
		})
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	if s.log != nil {
		s.log.Info("monthly_report_scheduler_tick", nil)
	}

	if err := s.email.SendMonthlyUsageReportIfNotSent(ctx, time.Now()); err != nil {
		if s.log != nil {
			s.log.Error("monthly_report_scheduler_failed", map[string]any{
				"error": err.Error(),
			})
		}
		return
	}

	if s.log != nil {
		s.log.Info("monthly_report_scheduler_checked", nil)
	}
}
