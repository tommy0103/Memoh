-- name: CreateScheduleLog :one
INSERT INTO schedule_logs (schedule_id, bot_id, session_id, started_at)
VALUES ($1, $2, sqlc.narg(session_id)::uuid, now())
RETURNING id, schedule_id, bot_id, session_id, status, result_text, error_message, error_code, usage, started_at, completed_at;

-- name: CompleteScheduleLog :one
UPDATE schedule_logs
SET status = $2,
    result_text = $3,
    error_message = '',
    error_code = $4,
    usage = $5,
    model_id = $6,
    completed_at = now()
WHERE team_id = public.memoh_current_team_id() AND id = $1
RETURNING id, schedule_id, bot_id, session_id, status, result_text, error_message, error_code, usage, model_id, started_at, completed_at, team_id;

-- name: ListScheduleLogsByBot :many
SELECT id, schedule_id, bot_id, session_id, status, result_text, error_message, error_code, usage, started_at, completed_at
FROM schedule_logs
WHERE team_id = public.memoh_current_team_id() AND bot_id = $1
ORDER BY started_at DESC
LIMIT $2 OFFSET $3;

-- name: CountScheduleLogsByBot :one
SELECT count(*) FROM schedule_logs WHERE team_id = public.memoh_current_team_id() AND bot_id = $1;

-- name: ListScheduleLogsBySchedule :many
SELECT id, schedule_id, bot_id, session_id, status, result_text, error_message, error_code, usage, started_at, completed_at
FROM schedule_logs
WHERE team_id = public.memoh_current_team_id() AND schedule_id = $1
ORDER BY started_at DESC
LIMIT $2 OFFSET $3;

-- name: CountScheduleLogsBySchedule :one
SELECT count(*) FROM schedule_logs WHERE team_id = public.memoh_current_team_id() AND schedule_id = $1;

-- name: DeleteScheduleLogsByBot :exec
DELETE FROM schedule_logs WHERE team_id = public.memoh_current_team_id() AND bot_id = $1;

-- name: DeleteScheduleLogsBySchedule :exec
DELETE FROM schedule_logs WHERE team_id = public.memoh_current_team_id() AND schedule_id = $1;

-- name: ListScheduleRunSessions :many
-- One row per fire of a schedule with the title of the session it ran in,
-- newest first. A fire whose session was soft-deleted is dropped by the join;
-- a hard-deleted session nulls session_id (ON DELETE SET NULL), so a NULL
-- session on a successful fire also means "deleted" and is dropped. A failed
-- fire with no session never got one and is kept so the failure stays visible.
-- The keyset cursor is (started_at, id) of the last row of the previous page.
SELECT
  l.id, l.session_id, l.status, l.error_code, l.started_at, l.completed_at,
  COALESCE(s.title, '')::text AS session_title
FROM schedule_logs l
LEFT JOIN bot_sessions s ON s.id = l.session_id AND s.deleted_at IS NULL
WHERE l.team_id = public.memoh_current_team_id()
  AND l.schedule_id = sqlc.arg(schedule_id)
  AND (s.id IS NOT NULL OR (l.session_id IS NULL AND l.status = 'error'))
  AND (
    NOT sqlc.arg(use_cursor)::bool
    OR (l.started_at, l.id) < (sqlc.arg(cursor_started_at)::timestamptz, sqlc.arg(cursor_id)::uuid)
  )
ORDER BY l.started_at DESC, l.id DESC
LIMIT sqlc.arg(limit_count)::int;
