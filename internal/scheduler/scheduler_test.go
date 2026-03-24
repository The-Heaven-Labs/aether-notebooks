package scheduler_test

import (
	"testing"

	"github.com/heavenlabs/hnb/internal/scheduler"
)

func TestNextRunTime(t *testing.T) {
	next, err := scheduler.NextRun("0 9 * * *")
	if err != nil {
		t.Fatalf("next run: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero next run time")
	}
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Fatalf("expected 09:00, got %02d:%02d", next.Hour(), next.Minute())
	}
}

func TestNextRunInvalidExpr(t *testing.T) {
	_, err := scheduler.NextRun("not-a-cron")
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}
