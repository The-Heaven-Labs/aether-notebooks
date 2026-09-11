package agent_test

import (
	"testing"

	"github.com/the-heaven-labs/aether/internal/agent"
)

// Events on a session stream carry monotonically increasing sequence numbers.
func TestStreamSeqMonotonic(t *testing.T) {
	sm := agent.NewStreamManager()
	sub, unsub := sm.Subscribe("sess-1", 16, true)
	defer unsub()

	for i := 0; i < 5; i++ {
		sm.Publish("sess-1", map[string]any{"type": "token", "data": "x"})
	}
	var seqs []uint64
	for i := 0; i < 5; i++ {
		evt := <-sub
		seqs = append(seqs, evt.Seq)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Fatalf("seq not monotonic: %v", seqs)
		}
	}
	if got := sm.LastSeq("sess-1"); got != seqs[len(seqs)-1] {
		t.Fatalf("LastSeq = %d, want %d", got, seqs[len(seqs)-1])
	}
}

// A reconnecting subscriber replays the buffer with original seq numbers.
func TestStreamReplayPreservesSeq(t *testing.T) {
	sm := agent.NewStreamManager()
	sub1, unsub1 := sm.Subscribe("sess-1", 16, true)
	sm.Publish("sess-1", map[string]any{"type": "token"})
	first := <-sub1
	unsub1()

	sub2, unsub2 := sm.Subscribe("sess-1", 16, false)
	defer unsub2()
	replayed := <-sub2
	if replayed.Seq != first.Seq {
		t.Fatalf("replayed seq = %d, want %d", replayed.Seq, first.Seq)
	}
}

// skipBuffer=true disables replay (live-only subscription).
func TestStreamSkipBuffer(t *testing.T) {
	sm := agent.NewStreamManager()
	sub1, unsub1 := sm.Subscribe("sess-1", 16, true)
	sm.Publish("sess-1", map[string]any{"type": "token"})
	<-sub1
	unsub1()

	sub2, unsub2 := sm.Subscribe("sess-1", 16, true)
	defer unsub2()
	select {
	case evt := <-sub2:
		t.Fatalf("expected no replay with skipBuffer=true, got seq %d", evt.Seq)
	default:
	}
}

// A slow subscriber never causes silent divergence: the drop is counted and a
// resync marker is queued so the client reconciles via reconnect.
func TestStreamDropEmitsResyncMarker(t *testing.T) {
	sm := agent.NewStreamManager()
	// Buffer of 1: first publish fills it, second triggers the drop path.
	sub, unsub := sm.Subscribe("sess-1", 1, true)
	defer unsub()

	sm.Publish("sess-1", map[string]any{"type": "token", "n": 1})
	sm.Publish("sess-1", map[string]any{"type": "token", "n": 2})

	if got := sm.DropCount("sess-1"); got == 0 {
		t.Fatal("expected drop to be counted")
	}
	// After the drop path (evict oldest + queue marker) exactly one event
	// remains: the resync marker.
	evt := <-sub
	m, ok := evt.Msg.(map[string]any)
	if !ok || m["type"] != "resync" {
		t.Fatalf("expected a resync marker after drop, got %#v", evt.Msg)
	}
}

// Publishing to an unknown session is a no-op (no stream, no panic).
func TestStreamPublishWithoutSubscribers(t *testing.T) {
	sm := agent.NewStreamManager()
	sm.Publish("nope", map[string]any{"type": "token"})
	if got := sm.LastSeq("nope"); got != 0 {
		t.Fatalf("LastSeq = %d, want 0", got)
	}
}
