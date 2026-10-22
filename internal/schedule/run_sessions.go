package schedule

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

const (
	runSessionsDefaultLimit = 20
	runSessionsMaxLimit     = 100
)

// ListRunSessions returns one page of a schedule's fires, newest first, each
// with the session it ran in. A fire whose session was deleted is left out; a
// fire that failed before it had a session is kept. A schedule that belongs to
// another bot answers ErrScheduleNotFound, exactly like an unknown one, so
// schedule ids cannot be probed across bots. limit outside 1..100 falls back to
// 20 or is capped at 100. cursor is the NextCursor of the previous page, or
// empty for the first page; a malformed cursor is an invalid "cursor" field.
func (s *Service) ListRunSessions(ctx context.Context, botID, scheduleID string, limit int, cursor string) (ListRunSessionsResponse, error) {
	sched, err := s.Get(ctx, scheduleID)
	if err != nil {
		return ListRunSessionsResponse{}, err
	}
	if sched.BotID != strings.TrimSpace(botID) {
		return ListRunSessionsResponse{}, ErrScheduleNotFound
	}
	after, err := decodeRunCursor(cursor)
	if err != nil {
		return ListRunSessionsResponse{}, err
	}
	switch {
	case limit <= 0:
		limit = runSessionsDefaultLimit
	case limit > runSessionsMaxLimit:
		limit = runSessionsMaxLimit
	}
	// One row past the page answers "is there more?" without a trailing
	// empty request.
	rows, err := s.queries.ListScheduleRunSessions(ctx, sqlc.ListScheduleRunSessionsParams{
		ScheduleID:      toUUID(sched.ID),
		UseCursor:       after.id.Valid,
		CursorStartedAt: pgtype.Timestamptz{Time: after.startedAt, Valid: after.id.Valid},
		CursorID:        after.id,
		LimitCount:      int32(limit + 1), //nolint:gosec // capped to 101 above
	})
	if err != nil {
		return ListRunSessionsResponse{}, err
	}
	resp := ListRunSessionsResponse{Items: make([]RunSession, 0, min(len(rows), limit))}
	for i, row := range rows {
		if i == limit {
			last := rows[limit-1]
			resp.NextCursor = encodeRunCursor(runCursor{startedAt: last.StartedAt.Time, id: last.ID})
			break
		}
		resp.Items = append(resp.Items, toRunSession(row))
	}
	return resp, nil
}

func toRunSession(row sqlc.ListScheduleRunSessionsRow) RunSession {
	item := RunSession{
		LogID:     row.ID.String(),
		Title:     row.SessionTitle,
		Status:    row.Status,
		ErrorCode: row.ErrorCode,
		StartedAt: row.StartedAt.Time,
	}
	if row.SessionID.Valid {
		item.SessionID = row.SessionID.String()
	}
	if row.CompletedAt.Valid {
		t := row.CompletedAt.Time
		item.CompletedAt = &t
	} else {
		item.Status = RunStatusRunning
	}
	return item
}

// runCursor is the keyset position (started_at, id) of the last run on a page.
type runCursor struct {
	startedAt time.Time
	id        pgtype.UUID
}

// encodeRunCursor packs the cursor as base64url(RFC3339Nano started_at|log id),
// the same shape the session list uses.
func encodeRunCursor(c runCursor) string {
	raw := c.startedAt.UTC().Format(time.RFC3339Nano) + "|" + c.id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeRunCursor(raw string) (runCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return runCursor{}, nil
	}
	invalid := invalidFieldf("cursor", "invalid cursor")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return runCursor{}, invalid
	}
	startedAt, id, ok := strings.Cut(string(decoded), "|")
	if !ok {
		return runCursor{}, invalid
	}
	at, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return runCursor{}, invalid
	}
	pgID, err := db.ParseUUID(id)
	if err != nil || !pgID.Valid {
		return runCursor{}, invalid
	}
	return runCursor{startedAt: at, id: pgID}, nil
}
