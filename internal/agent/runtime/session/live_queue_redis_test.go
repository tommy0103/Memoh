package sessionruntime

import (
	"context"
	"os"
	"testing"
	"time"
)

// Redis is optional in the normal unit-test environment. When configured, this
// contract deliberately uses two backend instances with one prefix: a queue
// operation that only works inside one process is not a valid Redis backend.
func TestRedisLiveQueueContractOptional(t *testing.T) {
	redisURL := os.Getenv("MEMOH_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = os.Getenv("MEMOH_TEST_VALKEY_URL")
	}
	if redisURL == "" {
		if os.Getenv("MEMOH_TEST_DISTRIBUTED_REQUIRED") == "1" {
			t.Fatal("distributed queue contract requires MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL")
		}
		t.Skip("set MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL to run the Redis live queue contract")
	}

	prefix := uniqueRuntimeBackendPrefix("live-queue")
	newBackend := func() *RedisBackend {
		backend, err := NewRedisBackend(context.Background(), RedisOptions{
			URL: redisURL, KeyPrefix: prefix, StateTTL: time.Minute,
		})
		if err != nil {
			t.Fatalf("redis backend: %v", err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		return backend
	}
	first, second := newBackend(), newBackend()
	ctx := context.Background()
	key := Key{BotID: "bot-live-queue", SessionID: "session-live-queue"}
	ref := RunRef{
		BotID: key.BotID, SessionID: key.SessionID, RunID: "run-live-queue",
		OwnerID: "owner-live-queue", Generation: "generation-live-queue", FencingToken: 41,
	}
	_, changed, err := first.StartRun(ctx, key, ref, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
		snapshot.BotID = key.BotID
		snapshot.SessionID = key.SessionID
		snapshot.CurrentRunView = &CurrentRunView{
			RunID: ref.RunID, OwnerID: ref.OwnerID, Generation: ref.Generation,
			SteerSupported: true, Status: RunStatusRunning,
		}
		return snapshot, true, nil
	})
	if err != nil || !changed {
		t.Fatalf("seed active run: changed=%v err=%v", changed, err)
	}
	handle := RunHandle{
		BotID: key.BotID, SessionID: key.SessionID, RunID: ref.RunID,
		OwnerID: ref.OwnerID, Generation: ref.Generation, FencingToken: ref.FencingToken,
	}

	runLiveQueueContract(t, first, second, key, handle)
}
