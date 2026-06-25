package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/notify"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type memAlertStore struct {
	mu   sync.Mutex
	sent map[string]bool // key: "period/threshold/type"
}

func (s *memAlertStore) key(periodStart time.Time, pct int, alertType string) string {
	return periodStart.Format(time.RFC3339) + "/" + string(rune('0'+pct)) + "/" + alertType
}

func (s *memAlertStore) WasSent(_ context.Context, periodStart time.Time, pct int, alertType string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[s.key(periodStart, pct, alertType)], nil
}

func (s *memAlertStore) MarkSent(_ context.Context, periodStart time.Time, pct int, alertType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent == nil {
		s.sent = make(map[string]bool)
	}
	s.sent[s.key(periodStart, pct, alertType)] = true
	return nil
}

type countingNotifier struct {
	mu    sync.Mutex
	calls int
}

func (n *countingNotifier) Send(_ context.Context, _ notify.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	return nil
}

func (n *countingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

// noopBudgetChecker satisfies BudgetChecker for gateways that need the field
// non-nil (emitMonthlyBudgetAlertOnce guards on g.budgetChecker != nil).
type noopBudgetChecker struct{}

func (noopBudgetChecker) CheckRequestBudget(_ context.Context, _ time.Time, _, _, _ string) error {
	return nil
}
func (noopBudgetChecker) CheckMonthlyBudget(_ context.Context, _ time.Time) error { return nil }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestEmitMonthlyBudgetAlertOnce_NilNotifier_DoesNotMarkSent verifies that
// when g.notifier == nil, alertStore.MarkSent is NOT called — so a notifier
// wired up later in the same billing period can still deliver the alert.
//
// Sequence tested:
//  1. Call with nil notifier → WasSent must still return false.
//  2. Wire a succeeding notifier → WasSent must return true and Send must have
//     been called exactly once.
func TestEmitMonthlyBudgetAlertOnce_NilNotifier_DoesNotMarkSent(t *testing.T) {
	store := &memAlertStore{}
	gw := &Gateway{
		alertStore:    store,
		budgetChecker: noopBudgetChecker{},
		notifier:      nil,
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Step 1: no notifier — threshold log fires but MarkSent must be skipped.
	gw.emitMonthlyBudgetAlertOnce(ctx, now, 80)

	wasSent, err := store.WasSent(ctx, periodStart, 80, "monthly")
	if err != nil {
		t.Fatalf("WasSent: %v", err)
	}
	if wasSent {
		t.Fatal("MarkSent was called with nil notifier; alert will be permanently suppressed")
	}

	// Step 2: wire a succeeding notifier — same period, same threshold.
	fakeN := &countingNotifier{}
	gw.notifier = fakeN

	gw.emitMonthlyBudgetAlertOnce(ctx, now, 80)

	if fakeN.count() != 1 {
		t.Errorf("notifier.Send calls: got %d, want 1", fakeN.count())
	}
	wasSent, err = store.WasSent(ctx, periodStart, 80, "monthly")
	if err != nil {
		t.Fatalf("WasSent: %v", err)
	}
	if !wasSent {
		t.Error("MarkSent was not called after successful delivery; alert would fire again on every subsequent request")
	}
}

// TestEmitMonthlyBudgetAlertOnce_WithNotifier_MarksSentOnSuccess verifies the
// existing working path: when g.notifier != nil and Send succeeds, MarkSent is
// called and subsequent calls for the same threshold/period are no-ops.
func TestEmitMonthlyBudgetAlertOnce_WithNotifier_MarksSentOnSuccess(t *testing.T) {
	store := &memAlertStore{}
	fakeN := &countingNotifier{}
	gw := &Gateway{
		alertStore:    store,
		budgetChecker: noopBudgetChecker{},
		notifier:      fakeN,
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	gw.emitMonthlyBudgetAlertOnce(ctx, now, 90)

	if fakeN.count() != 1 {
		t.Errorf("first call: notifier.Send calls: got %d, want 1", fakeN.count())
	}
	wasSent, _ := store.WasSent(ctx, periodStart, 90, "monthly")
	if !wasSent {
		t.Error("first call: MarkSent should have been called on success")
	}

	// Second call for same threshold/period — WasSent returns true, so Send must
	// not be called again.
	gw.emitMonthlyBudgetAlertOnce(ctx, now, 90)
	if fakeN.count() != 1 {
		t.Errorf("second call: notifier.Send calls: got %d, want 1 (dedup)", fakeN.count())
	}
}
