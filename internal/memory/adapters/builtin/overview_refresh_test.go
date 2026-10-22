package builtin

import (
	"context"
	"testing"
	"time"

	adapters "github.com/felinics/memoh/internal/memory/adapters"
)

// refreshingStore is a memory store that defers MEMORY.md rewrites.
type refreshingStore struct {
	*fakeStore
	stale     bool
	since     time.Time
	refreshes int
}

func (s *refreshingStore) OverviewStaleSince(string) (time.Time, bool) { return s.since, s.stale }

func (s *refreshingStore) RefreshOverview(context.Context, string) error {
	s.refreshes++
	s.stale = false
	return nil
}

func TestBuiltinProviderRefreshesOverviewThroughGraphRuntime(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	store := &refreshingStore{fakeStore: newFakeStore(), stale: true, since: since}
	provider := NewBuiltinProvider(nil, NewGraphRuntime(nil, nil, store))
	var refresher adapters.OverviewRefresher = provider

	if got, stale := refresher.OverviewStaleSince("bot-1"); !stale || !got.Equal(since) {
		t.Fatalf("OverviewStaleSince = (%v, %v), want the store's (%v, true)", got, stale, since)
	}
	if err := refresher.RefreshOverview(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RefreshOverview: %v", err)
	}
	if _, stale := refresher.OverviewStaleSince("bot-1"); store.refreshes != 1 || stale {
		t.Fatalf("refreshes = %d stale = %v, want one refresh that clears the stale overview", store.refreshes, stale)
	}
}

func TestBuiltinProviderWithoutDeferringStoreNeverReportsStaleOverview(t *testing.T) {
	t.Parallel()

	provider := NewBuiltinProvider(nil, NewGraphRuntime(nil, nil, newFakeStore()))

	if _, stale := provider.OverviewStaleSince("bot-1"); stale {
		t.Fatal("a store that rewrites MEMORY.md on every write never lags behind")
	}
	if err := provider.RefreshOverview(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RefreshOverview: %v", err)
	}
}
