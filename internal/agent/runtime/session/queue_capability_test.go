package sessionruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/runtime/native"
)

func TestSteerRequiresConsumerAndRejectsFinishingRun(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		supported    bool
	}{
		{"old or unsupported owner", RunStatusRunning, false},
		{"terminal proposal", RunStatusFinishing, true},
		{"abort in progress", RunStatusAborting, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, key, _ := liveQueueFixture(t)
			ctx := context.Background()
			follow, err := backend.EnqueueFollowUp(ctx, key, "follow", "follow-invocation", []byte(`{"text":"later"}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := backend.Update(ctx, key, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
				snapshot.CurrentRunView.Status = tc.status
				snapshot.CurrentRunView.SteerSupported = tc.supported
				return snapshot, true, nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.EnqueueSteer(ctx, key, "steer", "steer-invocation", []byte(`{"text":"now"}`)); !errors.Is(err, ErrQueueSteerUnsupported) {
				t.Fatalf("steer admission=%v", err)
			}
			if _, err := backend.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: follow.ID}); !errors.Is(err, ErrQueueSteerUnsupported) {
				t.Fatalf("promotion=%v", err)
			}
			steers, follows, err := backend.PendingQueues(ctx, key, 0)
			if err != nil || len(steers) != 0 || len(follows) != 1 {
				t.Fatalf("rejected promotion changed queues: %v %v %v", steers, follows, err)
			}
		})
	}
}

func TestContinuationStepIndexUsesCurrentOwnerCursor(t *testing.T) {
	f := newAdmitFixture(t)
	ctx := context.Background()
	admission, err := f.manager.Admit(ctx, f.input("step-cursor", `{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.HandleAgentEvent(ctx, admission.Handle, native.StreamEvent{Type: native.EventStepEnd, StepNumber: 4}); err != nil {
		t.Fatal(err)
	}
	if index, err := f.manager.ContinuationStepIndex(admission.Handle); err != nil || index != 5 {
		t.Fatalf("continuation cursor=%d err=%v", index, err)
	}
	stale := admission.Handle
	stale.Generation = "old-generation"
	if _, err := f.manager.ContinuationStepIndex(stale); !errors.Is(err, ErrRunOwnershipLost) {
		t.Fatalf("stale cursor lookup=%v", err)
	}
	f.finish(t, admission)
}
