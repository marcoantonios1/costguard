package health

import (
	"sync"
	"testing"
	"time"
)

func outcome(success bool, latencyMS int, errCategory string) Outcome {
	return Outcome{
		Success:       success,
		Latency:       time.Duration(latencyMS) * time.Millisecond,
		Timestamp:     time.Now(),
		ErrorCategory: errCategory,
	}
}

func TestSnapshot_Empty(t *testing.T) {
	tr := New(100)
	s := tr.Snapshot("unknown")
	if s.SuccessRate != -1 {
		t.Errorf("SuccessRate: got %v, want -1", s.SuccessRate)
	}
	if s.AvgLatencyMS != -1 {
		t.Errorf("AvgLatencyMS: got %v, want -1", s.AvgLatencyMS)
	}
	if s.Total != 0 {
		t.Errorf("Total: got %d, want 0", s.Total)
	}
}

func TestSnapshot_AllSuccesses(t *testing.T) {
	tr := New(100)
	for i := 0; i < 5; i++ {
		tr.Record("p", outcome(true, 50, ""))
	}
	s := tr.Snapshot("p")
	if s.SuccessRate != 1.0 {
		t.Errorf("SuccessRate: got %v, want 1.0", s.SuccessRate)
	}
	if s.Successes != 5 {
		t.Errorf("Successes: got %d, want 5", s.Successes)
	}
	if s.Failures != 0 {
		t.Errorf("Failures: got %d, want 0", s.Failures)
	}
	if s.Total != 5 {
		t.Errorf("Total: got %d, want 5", s.Total)
	}
}

func TestSnapshot_AllFailures(t *testing.T) {
	tr := New(100)
	for i := 0; i < 3; i++ {
		tr.Record("p", outcome(false, 0, "upstream_failure"))
	}
	s := tr.Snapshot("p")
	if s.SuccessRate != 0.0 {
		t.Errorf("SuccessRate: got %v, want 0.0", s.SuccessRate)
	}
	if s.Failures != 3 {
		t.Errorf("Failures: got %d, want 3", s.Failures)
	}
}

func TestSnapshot_MixedRate(t *testing.T) {
	tr := New(100)
	tr.Record("p", outcome(true, 10, ""))
	tr.Record("p", outcome(true, 20, ""))
	tr.Record("p", outcome(true, 30, ""))
	tr.Record("p", outcome(false, 0, "rate_limit"))
	s := tr.Snapshot("p")
	if s.Total != 4 {
		t.Errorf("Total: got %d, want 4", s.Total)
	}
	if s.SuccessRate != 0.75 {
		t.Errorf("SuccessRate: got %v, want 0.75", s.SuccessRate)
	}
	if s.Failures != 1 {
		t.Errorf("Failures: got %d, want 1", s.Failures)
	}
}

func TestSnapshot_RingOverflow(t *testing.T) {
	tr := New(10)
	for i := 0; i < 15; i++ {
		tr.Record("p", outcome(true, 1, ""))
	}
	s := tr.Snapshot("p")
	if s.Total != 10 {
		t.Errorf("Total: got %d, want 10 (ring overflow evicts oldest)", s.Total)
	}
	if s.WindowSize != 10 {
		t.Errorf("WindowSize: got %d, want 10", s.WindowSize)
	}
}

func TestSnapshot_LastSuccessAndFailure(t *testing.T) {
	tr := New(100)
	t1 := time.Now().Add(-2 * time.Second)
	t2 := time.Now().Add(-1 * time.Second)
	t3 := time.Now()

	tr.Record("p", Outcome{Success: true, Timestamp: t1})
	tr.Record("p", Outcome{Success: false, Timestamp: t2, ErrorCategory: "auth"})
	tr.Record("p", Outcome{Success: true, Timestamp: t3})

	s := tr.Snapshot("p")
	if !s.LastSuccess.Equal(t3) {
		t.Errorf("LastSuccess: got %v, want %v", s.LastSuccess, t3)
	}
	if !s.LastFailure.Equal(t2) {
		t.Errorf("LastFailure: got %v, want %v", s.LastFailure, t2)
	}
}

func TestSnapshot_LastError(t *testing.T) {
	tr := New(100)
	tr.Record("p", Outcome{Success: false, Timestamp: time.Now().Add(-time.Second), ErrorCategory: "auth"})
	tr.Record("p", Outcome{Success: false, Timestamp: time.Now(), ErrorCategory: "rate_limit"})

	s := tr.Snapshot("p")
	if s.LastError != "rate_limit" {
		t.Errorf("LastError: got %q, want rate_limit", s.LastError)
	}
}

func TestSnapshot_AvgLatencySuccessOnly(t *testing.T) {
	tr := New(100)
	tr.Record("p", Outcome{Success: true, Latency: 100 * time.Millisecond, Timestamp: time.Now()})
	tr.Record("p", Outcome{Success: true, Latency: 200 * time.Millisecond, Timestamp: time.Now()})
	tr.Record("p", Outcome{Success: false, Latency: 999 * time.Millisecond, Timestamp: time.Now()}) // excluded

	s := tr.Snapshot("p")
	if s.AvgLatencyMS != 150 {
		t.Errorf("AvgLatencyMS: got %v, want 150 (failures excluded)", s.AvgLatencyMS)
	}
}

func TestSnapshot_AvgLatencyNoSuccesses(t *testing.T) {
	tr := New(100)
	tr.Record("p", Outcome{Success: false, Latency: 50 * time.Millisecond, Timestamp: time.Now()})
	s := tr.Snapshot("p")
	if s.AvgLatencyMS != -1 {
		t.Errorf("AvgLatencyMS: got %v, want -1 (no successes)", s.AvgLatencyMS)
	}
}

func TestSnapshots_SortedByName(t *testing.T) {
	tr := New(100)
	tr.Record("zebra", outcome(true, 1, ""))
	tr.Record("alpha", outcome(true, 1, ""))
	tr.Record("mango", outcome(true, 1, ""))

	snaps := tr.Snapshots()
	if len(snaps) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(snaps))
	}
	names := []string{snaps[0].Provider, snaps[1].Provider, snaps[2].Provider}
	want := []string{"alpha", "mango", "zebra"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("snaps[%d].Provider: got %q, want %q", i, n, want[i])
		}
	}
}

func TestSnapshots_OnlyProviderWithData(t *testing.T) {
	tr := New(100)
	tr.Record("active", outcome(true, 1, ""))
	// "inactive" never recorded

	snaps := tr.Snapshots()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].Provider != "active" {
		t.Errorf("Provider: got %q, want active", snaps[0].Provider)
	}
}

func TestConcurrentRecordAndSnapshot(t *testing.T) {
	tr := New(50)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tr.Record("p", outcome(true, 10, ""))
		}()
		go func() {
			defer wg.Done()
			_ = tr.Snapshot("p")
		}()
	}
	wg.Wait()
}
