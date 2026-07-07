package feedback_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/feedback"
)

func writeRecordLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("write line: %v", err)
		}
	}
}

func marshalRecord(t *testing.T, r feedback.FeedbackRecord) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return string(b)
}

func TestReader_ReadAllNoFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.jsonl")

	now := time.Now().UTC()
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, marshalRecord(t, feedback.FeedbackRecord{
			Consumer:  "svc",
			Model:     "model-a",
			Outcome:   "success",
			Timestamp: now,
		}))
	}
	writeRecordLines(t, path, lines...)

	rd := feedback.NewReader(path)
	records, err := rd.Read(context.Background(), feedback.Filter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}
}

func TestReader_FilterByModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.jsonl")

	lines := []string{
		marshalRecord(t, feedback.FeedbackRecord{Consumer: "svc", Model: "model-a", Outcome: "success"}),
		marshalRecord(t, feedback.FeedbackRecord{Consumer: "svc", Model: "model-b", Outcome: "success"}),
		marshalRecord(t, feedback.FeedbackRecord{Consumer: "svc", Model: "model-a", Outcome: "failure"}),
	}
	writeRecordLines(t, path, lines...)

	rd := feedback.NewReader(path)
	records, err := rd.Read(context.Background(), feedback.Filter{Model: "model-a"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	for _, r := range records {
		if r.Model != "model-a" {
			t.Errorf("expected model-a, got %s", r.Model)
		}
	}
}

func TestReader_LastN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.jsonl")

	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, marshalRecord(t, feedback.FeedbackRecord{
			Consumer: "svc",
			Model:    "model-a",
			Outcome:  "success",
			// DurationMS used as a distinguishing marker for ordering.
			DurationMS: i,
		}))
	}
	writeRecordLines(t, path, lines...)

	rd := feedback.NewReader(path)
	records, err := rd.Read(context.Background(), feedback.Filter{LastN: 2})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].DurationMS != 3 || records[1].DurationMS != 4 {
		t.Errorf("expected the last 2 records (markers 3,4), got markers %d,%d", records[0].DurationMS, records[1].DurationMS)
	}
}

func TestReader_NonExistentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")

	rd := feedback.NewReader(path)
	records, err := rd.Read(context.Background(), feedback.Filter{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if records != nil {
		t.Fatalf("expected nil records, got %v", records)
	}
}

func TestReader_SkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.jsonl")

	lines := []string{
		marshalRecord(t, feedback.FeedbackRecord{Consumer: "svc-1", Outcome: "success"}),
		"{not valid json",
		marshalRecord(t, feedback.FeedbackRecord{Consumer: "svc-2", Outcome: "success"}),
	}
	writeRecordLines(t, path, lines...)

	rd := feedback.NewReader(path)
	records, err := rd.Read(context.Background(), feedback.Filter{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Consumer != "svc-1" || records[1].Consumer != "svc-2" {
		t.Errorf("unexpected records: %+v", records)
	}
}
