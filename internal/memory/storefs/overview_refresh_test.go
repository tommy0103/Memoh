package storefs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/workspace/bridge"
)

var errWorkspaceUnavailable = errors.New("workspace unavailable")

// recordingWorkspace fails every workspace access, so a test can tell whether
// the Service tried to touch MEMORY.md and with which context.
type recordingWorkspace struct {
	calls int
	ctxs  []context.Context
}

func (w *recordingWorkspace) MCPClient(ctx context.Context, _ string) (*bridge.Client, error) {
	w.calls++
	w.ctxs = append(w.ctxs, ctx)
	return nil, errWorkspaceUnavailable
}

func overviewStale(s *Service, botID string) bool {
	_, stale := s.OverviewStaleSince(botID)
	return stale
}

func TestConversationWriteDefersOverview(t *testing.T) {
	t.Parallel()

	workspace := &recordingWorkspace{}
	s := New(nil, workspace)

	if err := s.SyncOverview(WithConversationWrite(context.Background()), "bot-1"); err != nil {
		t.Fatalf("SyncOverview: %v", err)
	}
	if workspace.calls != 0 {
		t.Fatalf("workspace calls = %d, want the MEMORY.md rewrite deferred", workspace.calls)
	}
	if !overviewStale(s, "bot-1") {
		t.Fatal("deferred rewrite did not mark the overview stale")
	}
}

func TestUnmarkedWriteRewritesOverviewImmediately(t *testing.T) {
	t.Parallel()

	workspace := &recordingWorkspace{}
	s := New(nil, workspace)

	if err := s.SyncOverview(context.Background(), "bot-1"); !errors.Is(err, errWorkspaceUnavailable) {
		t.Fatalf("SyncOverview error = %v, want a management write rewritten immediately", err)
	}
}

func TestRefreshOverviewRewritesStaleOverviewWithoutConversationMark(t *testing.T) {
	t.Parallel()

	workspace := &recordingWorkspace{}
	s := New(nil, workspace)
	if !overviewStale(s, "bot-1") {
		t.Fatal("a bot this process never wrote must count as stale")
	}

	err := s.RefreshOverview(WithConversationWrite(context.Background()), "bot-1")
	if !errors.Is(err, errWorkspaceUnavailable) {
		t.Fatalf("RefreshOverview error = %v, want the rewrite attempted", err)
	}
	if len(workspace.ctxs) == 0 || IsConversationWrite(workspace.ctxs[0]) {
		t.Fatal("refresh must write MEMORY.md, not defer it again")
	}
	if !overviewStale(s, "bot-1") {
		t.Fatal("a failed refresh must leave the overview stale")
	}
}

func TestOverviewCleanIgnoresRefreshSupersededByNewerWrite(t *testing.T) {
	t.Parallel()

	workspace := &recordingWorkspace{}
	s := New(nil, workspace)
	conversation := WithConversationWrite(context.Background())

	generation := s.overviewGeneration("bot-1")
	if !s.deferOverview(conversation, "bot-1") {
		t.Fatal("conversation write was not deferred")
	}
	s.markOverviewClean("bot-1", generation)
	if !overviewStale(s, "bot-1") {
		t.Fatal("a refresh that read the files before a newer write must not mark the overview clean")
	}

	s.markOverviewClean("bot-1", s.overviewGeneration("bot-1"))
	if overviewStale(s, "bot-1") {
		t.Fatal("overview should be clean after a refresh that saw every write")
	}
	if err := s.RefreshOverview(context.Background(), "bot-1"); err != nil {
		t.Fatalf("RefreshOverview on a clean overview: %v", err)
	}
	if workspace.calls != 0 {
		t.Fatalf("workspace calls = %d, want none for a clean overview", workspace.calls)
	}
}

func TestOverviewStaleSinceTracksFirstDeferredWrite(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	now := start
	previous := overviewNow
	overviewNow = func() time.Time { return now }
	t.Cleanup(func() { overviewNow = previous })

	workspace := &recordingWorkspace{}
	s := New(nil, workspace)
	conversation := WithConversationWrite(context.Background())

	s.markOverviewClean("bot-1", s.overviewGeneration("bot-1"))
	if since, stale := s.OverviewStaleSince("bot-1"); stale || !since.IsZero() {
		t.Fatalf("clean overview = (%v, %v), want not stale", since, stale)
	}
	s.deferOverview(conversation, "bot-1")
	now = start.Add(10 * time.Minute)
	s.deferOverview(conversation, "bot-1")
	if since, stale := s.OverviewStaleSince("bot-1"); !stale || !since.Equal(start) {
		t.Fatalf("stale since = (%v, %v), want the first deferred write at %v", since, stale, start)
	}

	s.markOverviewClean("bot-1", s.overviewGeneration("bot-1"))
	now = start.Add(20 * time.Minute)
	s.deferOverview(conversation, "bot-1")
	if since, _ := s.OverviewStaleSince("bot-1"); !since.Equal(now) {
		t.Fatalf("stale since = %v after a refresh, want the next deferred write at %v", since, now)
	}
}

func TestOverviewStaleSinceStartsWhenAnUnseenBotIsFirstAsked(t *testing.T) {
	start := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	now := start
	previous := overviewNow
	overviewNow = func() time.Time { return now }
	t.Cleanup(func() { overviewNow = previous })

	s := New(nil, &recordingWorkspace{})
	if since, stale := s.OverviewStaleSince("bot-1"); !stale || !since.Equal(start) {
		t.Fatalf("unseen bot = (%v, %v), want stale since %v", since, stale, start)
	}
	now = start.Add(time.Hour)
	if since, _ := s.OverviewStaleSince("bot-1"); !since.Equal(start) {
		t.Fatalf("stale since = %v on a later ask, want it kept at %v", since, start)
	}
}
