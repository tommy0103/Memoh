package storefs

import (
	"context"
	"time"
)

// overviewNow is the clock stale overviews are aged with; tests replace it.
var overviewNow = time.Now

type conversationWriteKey struct{}

// WithConversationWrite marks ctx as carrying a memory write made while a
// conversation runs: post-turn extraction or a memory tool the agent called.
// Such a write leaves MEMORY.md as it is until RefreshOverview runs: MEMORY.md
// sits in the system prompt ahead of the whole history, so rewriting it while
// a session is warm makes every following token of that session's next
// request miss the prompt cache. Writes outside a conversation (management
// API, ingest, rebuild) leave ctx unmarked and rewrite MEMORY.md at once.
func WithConversationWrite(ctx context.Context) context.Context {
	return context.WithValue(ctx, conversationWriteKey{}, true)
}

// IsConversationWrite reports whether ctx carries a conversation write.
func IsConversationWrite(ctx context.Context) bool {
	marked, _ := ctx.Value(conversationWriteKey{}).(bool)
	return marked
}

// overviewState is what the Service knows about a bot's MEMORY.md. A bot with
// no entry has not been written by this process, so its overview may already
// lag behind writes an earlier process deferred.
type overviewState struct {
	clean bool
	// staleSince is when MEMORY.md started lagging: the first write it
	// deferred, or, for a bot this process had not seen, when it first
	// asked. Zero while clean.
	staleSince time.Time
	// generation counts deferred writes, so a refresh that read the memory
	// files before a newer write landed does not mark the overview clean.
	generation uint64
}

// deferOverview reports whether the MEMORY.md rewrite for this write is
// deferred, recording that the overview now lags behind the memory files.
func (s *Service) deferOverview(ctx context.Context, botID string) bool {
	if !IsConversationWrite(ctx) {
		return false
	}
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	if s.overview == nil {
		s.overview = make(map[string]overviewState)
	}
	state := s.overview[botID]
	if state.staleSince.IsZero() {
		state.staleSince = overviewNow()
	}
	state.clean = false
	state.generation++
	s.overview[botID] = state
	return true
}

func (s *Service) overviewGeneration(botID string) uint64 {
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	return s.overview[botID].generation
}

func (s *Service) markOverviewClean(botID string, generation uint64) {
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	if s.overview == nil {
		s.overview = make(map[string]overviewState)
	}
	state := s.overview[botID]
	if state.generation != generation {
		return
	}
	state.clean = true
	state.staleSince = time.Time{}
	s.overview[botID] = state
}

// OverviewStaleSince reports whether MEMORY.md may lag behind the bot's memory
// files and, if so, since when.
func (s *Service) OverviewStaleSince(botID string) (time.Time, bool) {
	s.overviewMu.Lock()
	defer s.overviewMu.Unlock()
	state, known := s.overview[botID]
	if known && state.clean {
		return time.Time{}, false
	}
	if state.staleSince.IsZero() {
		if s.overview == nil {
			s.overview = make(map[string]overviewState)
		}
		state.staleSince = overviewNow()
		s.overview[botID] = state
	}
	return state.staleSince, true
}

// RefreshOverview rewrites MEMORY.md from the memory files when it may be
// stale. Rendering is deterministic, so a refresh that finds nothing new
// writes the same bytes the file already holds.
func (s *Service) RefreshOverview(ctx context.Context, botID string) error {
	if _, stale := s.OverviewStaleSince(botID); !stale {
		return nil
	}
	return s.writeOverview(context.WithValue(ctx, conversationWriteKey{}, false), botID)
}

// writeOverview renders MEMORY.md from the memory files currently on disk.
func (s *Service) writeOverview(ctx context.Context, botID string) error {
	generation := s.overviewGeneration(botID)
	items, err := s.ReadAllMemoryFiles(ctx, botID)
	if err != nil {
		return err
	}
	if err := s.writeFile(ctx, botID, memoryOverviewPath(), formatMemoryOverviewMD(items)); err != nil {
		return err
	}
	s.markOverviewClean(botID, generation)
	return nil
}
