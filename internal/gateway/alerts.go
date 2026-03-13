package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/marcoantonios1/costguard/internal/notify"
)

func (g *Gateway) emitMonthlyBudgetAlertOnce(
	ctx context.Context,
	now time.Time,
	thresholdPercent int,
) {
	if g.alertStore == nil || g.budgetChecker == nil {
		return
	}

	periodStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	alertType := "monthly"

	sent, err := g.alertStore.WasSent(ctx, periodStart, thresholdPercent, alertType)
	if err != nil {
		if g.log != nil {
			g.log.Error("budget_alert_check_failed", map[string]any{
				"threshold_percent": thresholdPercent,
				"error":             err.Error(),
			})
		}
		return
	}

	if sent {
		return
	}

	if g.log != nil {
		switch thresholdPercent {
		case 80:
			g.log.Warn("budget_80_percent_reached", map[string]any{
				"threshold_percent": thresholdPercent,
				"period_start":      periodStart.Format(time.RFC3339),
			})
		case 90:
			g.log.Warn("budget_90_percent_reached", map[string]any{
				"threshold_percent": thresholdPercent,
				"period_start":      periodStart.Format(time.RFC3339),
			})
		case 100:
			g.log.Error("budget_100_percent_reached", map[string]any{
				"threshold_percent": thresholdPercent,
				"period_start":      periodStart.Format(time.RFC3339),
			})
		}
	}

	emailSent := true
	if g.notifier != nil {
		if err := g.sendBudgetAlertEmail(ctx, thresholdPercent, periodStart); err != nil {
			emailSent = false
		}
	}

	if emailSent {
		if err := g.alertStore.MarkSent(ctx, periodStart, thresholdPercent, alertType); err != nil && g.log != nil {
			g.log.Error("budget_alert_mark_sent_failed", map[string]any{
				"threshold_percent": thresholdPercent,
				"error":             err.Error(),
			})
		}
	}
}

func (g *Gateway) sendBudgetAlertEmail(ctx context.Context, thresholdPercent int, periodStart time.Time) error {
	if g.notifier == nil {
		return fmt.Errorf("notifier not configured")
	}

	subject := fmt.Sprintf("[Costguard] Monthly budget reached %d%%", thresholdPercent)
	if thresholdPercent == 100 {
		subject = "[Costguard] Monthly budget exceeded"
	}

	body := fmt.Sprintf(
		"Costguard budget alert\n\nThreshold: %d%%\nPeriod start: %s\n",
		thresholdPercent,
		periodStart.Format("2006-01-02"),
	)

	err := g.notifier.Send(ctx, notify.Message{
		Subject: subject,
		Text:    body,
	})
	if err != nil && g.log != nil {
		g.log.Error("budget_alert_email_failed", map[string]any{
			"threshold_percent": thresholdPercent,
			"error":             err.Error(),
		})
		return err
	}

	if g.log != nil {
		g.log.Info("budget_alert_email_sent", map[string]any{
			"threshold_percent": thresholdPercent,
			"period_start":      periodStart.Format(time.RFC3339),
		})
	}
	return nil
}