package feedback_test

import (
	"math"
	"testing"

	"github.com/marcoantonios1/costguard/internal/feedback"
)

func assertFloatEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("%s: got non-finite value %v", name, got)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func TestAggregate_EmptyInput(t *testing.T) {
	stats := feedback.Aggregate(nil, feedback.Filter{Model: "m1"})

	if stats.TaskCount != 0 {
		t.Errorf("expected TaskCount 0, got %d", stats.TaskCount)
	}
	if stats.Model != "m1" {
		t.Errorf("expected Model m1 to be carried over from filter, got %q", stats.Model)
	}
	assertFloatEqual(t, "SuccessRate", stats.SuccessRate, 0)
	assertFloatEqual(t, "ReviewerPassRate", stats.ReviewerPassRate, 0)
	assertFloatEqual(t, "AvgReviewerRetries", stats.AvgReviewerRetries, 0)
	assertFloatEqual(t, "AvgDurationMS", stats.AvgDurationMS, 0)
	assertFloatEqual(t, "AvgInputTokens", stats.AvgInputTokens, 0)
	assertFloatEqual(t, "AvgOutputTokens", stats.AvgOutputTokens, 0)
}

func TestAggregate_KnownValues(t *testing.T) {
	records := []feedback.FeedbackRecord{
		{Outcome: "success", ReviewerPassed: true, ReviewerRetries: 1, DurationMS: 100, InputTokens: 10, OutputTokens: 20},
		{Outcome: "success", ReviewerPassed: true, ReviewerRetries: 0, DurationMS: 200, InputTokens: 20, OutputTokens: 40},
		{Outcome: "success", ReviewerPassed: false, ReviewerRetries: 3, DurationMS: 300, InputTokens: 30, OutputTokens: 60},
		{Outcome: "failure", ReviewerPassed: false, ReviewerRetries: 2, DurationMS: 400, InputTokens: 40, OutputTokens: 80},
	}

	stats := feedback.Aggregate(records, feedback.Filter{})

	if stats.TaskCount != 4 {
		t.Fatalf("expected TaskCount 4, got %d", stats.TaskCount)
	}
	assertFloatEqual(t, "SuccessRate", stats.SuccessRate, 3.0/4.0)
	assertFloatEqual(t, "ReviewerPassRate", stats.ReviewerPassRate, 2.0/4.0)
	assertFloatEqual(t, "AvgReviewerRetries", stats.AvgReviewerRetries, 6.0/4.0)
	assertFloatEqual(t, "AvgDurationMS", stats.AvgDurationMS, 1000.0/4.0)
	assertFloatEqual(t, "AvgInputTokens", stats.AvgInputTokens, 100.0/4.0)
	assertFloatEqual(t, "AvgOutputTokens", stats.AvgOutputTokens, 200.0/4.0)
}

func TestAggregate_CarriesFilterIdentity(t *testing.T) {
	records := []feedback.FeedbackRecord{
		{Outcome: "success"},
	}
	f := feedback.Filter{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b"}

	stats := feedback.Aggregate(records, f)

	if stats.Consumer != "forge" || stats.Role != "coder" || stats.Model != "qwen3-coder:30b" {
		t.Errorf("expected identity fields carried from filter, got %+v", stats)
	}
}

func TestAggregate_LastNAppliedBeforeAggregation(t *testing.T) {
	// Simulates a Filter{LastN: 2} already applied upstream (by Reader) to a
	// 5-record slice — Aggregate itself just aggregates whatever slice it's given.
	all := []feedback.FeedbackRecord{
		{Outcome: "failure", DurationMS: 10},
		{Outcome: "failure", DurationMS: 20},
		{Outcome: "failure", DurationMS: 30},
		{Outcome: "success", DurationMS: 40},
		{Outcome: "success", DurationMS: 50},
	}
	lastTwo := all[len(all)-2:]

	stats := feedback.Aggregate(lastTwo, feedback.Filter{LastN: 2})

	if stats.TaskCount != 2 {
		t.Fatalf("expected TaskCount 2, got %d", stats.TaskCount)
	}
	assertFloatEqual(t, "SuccessRate", stats.SuccessRate, 1.0)
	assertFloatEqual(t, "AvgDurationMS", stats.AvgDurationMS, 45.0)
}
