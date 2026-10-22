package sessionruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSteerWakeRetainsQueueAndFencesOwner(t *testing.T) {
	f := newAdmitFixture(t)
	ctx := context.Background()
	admitted, err := f.manager.Admit(ctx, f.input("steer", `{"text":"start"}`))
	if err != nil {
		t.Fatal(err)
	}
	handle := admitted.Handle
	if err := f.manager.EnableSteer(ctx, handle); err != nil {
		t.Fatal(err)
	}
	wake := f.manager.SteerWake(handle)
	key := handle.key()
	item, err := f.manager.EnqueueSteer(ctx, key, "one", "one", []byte(`{"text":"adjust"}`))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("accepted queue input did not wake its owner")
	}
	cmd := Command{
		Type: CommandSteerWake, BotID: handle.BotID, SessionID: handle.SessionID,
		RunID: handle.RunID, Generation: handle.Generation, FencingToken: handle.FencingToken,
	}
	for range 3 {
		if err := f.manager.applyRoutedCommand(ctx, cmd); err != nil {
			t.Fatal(err)
		}
	}
	<-wake
	select {
	case <-wake:
		t.Fatal("duplicate wakes did not coalesce")
	default:
	}
	cmd.Generation = "stale"
	if err := f.manager.applyRoutedCommand(ctx, cmd); !errors.Is(err, ErrCommandTargetNotActive) {
		t.Fatalf("stale owner wake: %v", err)
	}
	steers, _, err := f.manager.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 1 || steers[0].ID != item.ID || steers[0].Status != QueueAccepted {
		t.Fatalf("wake applied or lost input: %+v, %v", steers, err)
	}
	// Cancellation between acknowledgement and consumption leaves at most a
	// stale notification, never a second input or a resurrected queue item.
	if err := f.manager.CancelSteer(ctx, key, item.ID); err != nil {
		t.Fatal(err)
	}
	steers, _, err = f.manager.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 0 {
		t.Fatalf("cancelled input survived: %+v %v", steers, err)
	}
}
