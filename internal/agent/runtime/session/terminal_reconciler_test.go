package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryManagerRunsTerminalReconcilerUntilClose(t *testing.T) {
	t.Parallel()
	manager := NewManager(NewMemoryBackend(), Options{
		Ledger:        newFakeLedger(),
		OwnerLeaseTTL: 30 * time.Millisecond,
	})
	var calls atomic.Int64
	called := make(chan struct{}, 4)
	manager.SetTerminalReconciler(func(context.Context) error {
		calls.Add(1)
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("memory terminal reconciler did not run")
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	closedCalls := calls.Load()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != closedCalls {
		t.Fatalf("terminal reconciler calls after Close = %d, want %d", got, closedCalls)
	}
}

func runExpiredLeaseTerminalReceiptContract(t *testing.T, suite distributedRuntimeBackendContractSuite) {
	t.Helper()
	for _, receipt := range []int64{7, 0, 8} {
		t.Run(fmt.Sprintf("receipt-%d", receipt), func(t *testing.T) {
			b := suite.newBackend(t)
			t.Cleanup(func() { _ = b.Close() })
			ctx := context.Background()
			key := Key{BotID: "receipt-bot", SessionID: "receipt-session"}
			ref := RunRef{BotID: key.BotID, SessionID: key.SessionID, RunID: "receipt-run", OwnerID: "owner", Generation: "generation", FencingToken: 7}
			deadline := time.Now().Add(time.Minute)
			if _, changed, err := b.StartRun(ctx, key, ref, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
				snapshot = EmptySnapshot(key.BotID, key.SessionID)
				snapshot.CurrentRunView = &CurrentRunView{RunID: ref.RunID, OwnerID: ref.OwnerID, Generation: ref.Generation, FencingToken: receipt, Status: RunStatusFinishing, OwnerLeaseExpiresAt: &deadline}
				return snapshot, true, nil
			}); err != nil || !changed {
				t.Fatalf("reserve: changed=%v err=%v", changed, err)
			}
			// Expiry removes the lease key. Terminal reconciliation must prove the
			// token from the snapshot, without treating receipt retention as liveness.
			if _, err := b.DeleteRunRef(ctx, ref); err != nil {
				t.Fatal(err)
			}
			_, changed, err := b.ReconcileTerminalRun(ctx, key, ref, func(snapshot Snapshot, _ time.Time) (Snapshot, bool, error) {
				snapshot.CurrentRunView.Status = RunStatusCompleted
				snapshot.CurrentRunView.OwnerLeaseExpiresAt = nil
				return snapshot, true, nil
			})
			if receipt == 7 {
				if err != nil || !changed {
					t.Fatalf("matching receipt: changed=%v err=%v", changed, err)
				}
			} else if !errors.Is(err, ErrRunOwnershipLost) || changed {
				t.Fatalf("unproved/successor receipt was overwritten: changed=%v err=%v", changed, err)
			}
		})
	}
}
