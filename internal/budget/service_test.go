package budget_test

import (
	"context"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
)

// ---------------------------------------------------------------------------
// spyUsageReader — records which methods were called so tests can assert that
// GetTotalSpend is skipped when MonthlyUSD == 0.
// ---------------------------------------------------------------------------

type spyUsageReader struct {
	totalSpendCalls int
	totalSpend      float64

	agentSpend map[string]float64
	teamSpend  map[string]float64
	projSpend  map[string]float64
}

func (s *spyUsageReader) GetTotalSpend(_ context.Context, _, _ time.Time) (float64, error) {
	s.totalSpendCalls++
	return s.totalSpend, nil
}

func (s *spyUsageReader) GetSpendForTeam(_ context.Context, team string, _, _ time.Time) (float64, error) {
	return s.teamSpend[team], nil
}

func (s *spyUsageReader) GetSpendForProject(_ context.Context, project string, _, _ time.Time) (float64, error) {
	return s.projSpend[project], nil
}

func (s *spyUsageReader) GetSpendForAgent(_ context.Context, agent string, _, _ time.Time) (float64, error) {
	return s.agentSpend[agent], nil
}

var testNow = time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Tests: GetTotalSpend is skipped when MonthlyUSD == 0
// ---------------------------------------------------------------------------

// TestCheckRequestBudget_NoGlobalBudget_GetTotalSpendNotCalled verifies that
// when MonthlyUSD == 0, GetTotalSpend is never invoked even when an agent
// budget is configured and enforced.
func TestCheckRequestBudget_NoGlobalBudget_GetTotalSpendNotCalled(t *testing.T) {
	spy := &spyUsageReader{
		agentSpend: map[string]float64{"bot": 3.0},
	}
	svc := budget.NewService(spy, budget.Config{
		Enabled:    true,
		MonthlyUSD: 0, // no global budget
		Agents:     map[string]float64{"bot": 10.0},
	})

	err := svc.CheckRequestBudget(context.Background(), testNow, "", "", "bot")
	if err != nil {
		t.Fatalf("expected nil, got %v (spend 3 < limit 10)", err)
	}
	if spy.totalSpendCalls != 0 {
		t.Errorf("GetTotalSpend calls: got %d, want 0 (MonthlyUSD == 0)", spy.totalSpendCalls)
	}
}

// TestCheckRequestBudget_NoGlobalBudget_AgentLimitStillEnforced verifies that
// with MonthlyUSD == 0, agent budget enforcement still works correctly when
// the agent is over its limit.
func TestCheckRequestBudget_NoGlobalBudget_AgentLimitStillEnforced(t *testing.T) {
	spy := &spyUsageReader{
		agentSpend: map[string]float64{"bot": 15.0},
	}
	svc := budget.NewService(spy, budget.Config{
		Enabled:    true,
		MonthlyUSD: 0,
		Agents:     map[string]float64{"bot": 10.0},
	})

	err := svc.CheckRequestBudget(context.Background(), testNow, "", "", "bot")
	if err != budget.ErrAgentBudgetExceeded {
		t.Fatalf("expected budget.ErrAgentBudgetExceeded, got %v", err)
	}
	if spy.totalSpendCalls != 0 {
		t.Errorf("GetTotalSpend calls: got %d, want 0 (MonthlyUSD == 0)", spy.totalSpendCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: GetTotalSpend IS called when MonthlyUSD > 0 (existing behaviour)
// ---------------------------------------------------------------------------

// TestCheckRequestBudget_GlobalBudgetExceeded verifies that when MonthlyUSD > 0
// and spend is at or above the limit, GetTotalSpend is called and
// budget.ErrMonthlyBudgetExceeded is returned.
func TestCheckRequestBudget_GlobalBudgetExceeded(t *testing.T) {
	spy := &spyUsageReader{totalSpend: 110.0}
	svc := budget.NewService(spy, budget.Config{
		Enabled:    true,
		MonthlyUSD: 100.0,
	})

	err := svc.CheckRequestBudget(context.Background(), testNow, "", "", "")
	if err != budget.ErrMonthlyBudgetExceeded {
		t.Fatalf("expected budget.ErrMonthlyBudgetExceeded, got %v", err)
	}
	if spy.totalSpendCalls != 1 {
		t.Errorf("GetTotalSpend calls: got %d, want 1", spy.totalSpendCalls)
	}
}

// TestCheckRequestBudget_GlobalBudgetNotExceeded verifies that when MonthlyUSD > 0
// and spend is below the limit, GetTotalSpend is called and nil is returned.
func TestCheckRequestBudget_GlobalBudgetNotExceeded(t *testing.T) {
	spy := &spyUsageReader{totalSpend: 50.0}
	svc := budget.NewService(spy, budget.Config{
		Enabled:    true,
		MonthlyUSD: 100.0,
	})

	err := svc.CheckRequestBudget(context.Background(), testNow, "", "", "")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if spy.totalSpendCalls != 1 {
		t.Errorf("GetTotalSpend calls: got %d, want 1", spy.totalSpendCalls)
	}
}

// TestCheckRequestBudget_Disabled verifies that a disabled Service is a no-op.
func TestCheckRequestBudget_Disabled(t *testing.T) {
	spy := &spyUsageReader{totalSpend: 999.0}
	svc := budget.NewService(spy, budget.Config{
		Enabled:    false,
		MonthlyUSD: 100.0,
	})

	err := svc.CheckRequestBudget(context.Background(), testNow, "", "", "")
	if err != nil {
		t.Fatalf("expected nil for disabled service, got %v", err)
	}
	if spy.totalSpendCalls != 0 {
		t.Errorf("GetTotalSpend calls: got %d, want 0 (service disabled)", spy.totalSpendCalls)
	}
}
