# Session input queues

Steer and follow-up are live session-runtime queues. They are deliberately not
part of the PostgreSQL durable model.

## Runtime ownership

The configured `session_runtime.backend` selects the storage implementation:

- `memory` keeps queue state in the process heap. It is fast and has no
  external dependency, but all queue items are lost when the process exits.
- `redis` keeps queue state in Redis using one document per queue and session
  and optimistic transactions. It is shared by runtime instances, but it is
  still transient: Redis expiry, flush, or loss can discard items.

Both implementations expose the same runtime API (`LiveQueueBackend` in
`internal/agent/runtime/session/live_queue.go`). Steer and follow-up remain
separate Go types, methods, and Redis keys; an item from one queue cannot be
read or mutated through the other queue API.

## Item lifecycle

```text
accepted -> claimed -> applied
   |          |
   +----------+-----> rejected   (error_code set)
   +----------------> canceled
```

- `accepted`: the item is stored and pending. Only accepted items are listed,
  reordered, edited, or canceled.
- `claimed`: a consumer holds the item. A steer claim carries the run ID,
  owner, generation, fencing token, and a claim token; a follow-up claim carries
  the terminal run that triggered it and a claim token.
- `applied`: the item entered a model step whose history commit succeeded
  (steer), or started its continuation run (follow-up).
- `rejected`: the runtime refused the item after acceptance. `ErrorCode` names
  the stable reason; today the only reason is `queue_target_run_not_active`.
- `canceled`: the caller withdrew a pending item. Promoting a follow-up to a
  steer cancels the follow-up and creates a new steer item.

`expired` exists in the status vocabulary but no path writes it.

Invocation IDs provide best-effort replay protection for the lifetime of the
retained items: the same invocation with the same payload returns the existing
item, and a different payload returns `ErrQueueInvocationConflict`.

## Capacity and compaction

Each queue document is bounded in two ways:

- At most `MaxPendingQueueItems` (64) accepted items per queue and session.
  Enqueue beyond that returns `ErrQueueCapacityExceeded`, surfaced as
  `queue_capacity_exceeded` over HTTP and channel slash commands.
- After every mutation the document keeps all accepted and claimed items and
  only the newest 64 terminal items. Map entries that reference dropped items
  (promotion records, per-run follow-up claims) are removed with them.

Replay protection therefore covers roughly the last 64 completed submissions.

## Steer

A steer is bound to the run that was active when it was admitted.

History keeps its existing turn model: every user message opens a turn, and
the assistant and tool rows that answer it belong to that turn. An applied
steer is therefore persisted as its own turn, and the output that follows it
is filed under that turn, while the run ID does not change. A run can span
several turns; the live projection names the post-steer assistant segment
after the steer's turn as soon as that turn is known.

- Admission records the active run as `TargetRunID`. Without an active run,
  or after the run has been sealed, admission returns `ErrQueueNoActiveRun`.
- Native streaming admission/promotion wakes the fenced owner through the
  existing command transport. Notifications coalesce locally; pending queue
  state remains authoritative. Each provider admission also checks that state.
- During model sampling (including waiting for response headers), steer cancels
  only the current invocation. Once its SDK stream is quiescent, the application
  persists an interrupted checkpoint, applies any input in that checkpoint,
  and claims the next input. Execution continues with the same run and an
  advanced step cursor, without another agent-start or a run-abort event.
  Unfinished reasoning is projected as text rather than replaying incomplete
  provider signatures. Failure to quiesce or persist fails the run safely.
- The interruption gate closes before the SDK receives tool-call output or
  finish-step. Already admitted tools and decisions are not cancelled by steer;
  they keep their normal result/approval lifecycle before input is consumed.
- At each committed step the application applies the previously claimed steer
  and claims the next accepted steer for the same run. During a tool loop the
  claimed text is injected into the next model request; at a final step the
  claim reopens the same run with the steer as the next model input.
- A step that parks the run for a tool approval or user input applies the
  previous claim but does not claim a new one. The resumed invocation claims
  at its next committed step or interruption checkpoint, so no claim waits
  unapplied across the decision.
- `ClaimNextSteer` returns the run's existing unapplied claim before selecting
  a new item. When the run has been reclaimed by a new owner, the stored claim
  is advanced to the new owner, generation, and fencing token; the previous
  owner's reference no longer matches it.
- A final step that finds no steer seals the run (`ClosedRunID`), so a steer
  that arrives between the final commit and the terminal record is refused.
- When the run reaches any terminal state the terminal observer calls
  `CloseSteerRun`: every accepted or claimed steer targeting the run becomes
  `rejected` with `queue_target_run_not_active`, and the run is sealed.

A claim is valid only for the run's current owner, generation, and fencing
token; applying with a stale claim returns `ErrRunOwnershipLost`.

### Codex comparison

Reference: OpenAI Codex commit
[`1530f828cbaea015bc0fc53c0486e2f889a677f3`](https://github.com/openai/codex/tree/1530f828cbaea015bc0fc53c0486e2f889a677f3).
Its `codex_thread.rs::steer_turn` requires the expected active turn and cannot
start another turn. `session/turn_input.rs::steer_input` appends input atomically
to the active task; `session/input_queue.rs` exposes queue activity notifications.
The public `turn/steer` contract is distinct from `turn/interrupt`:
https://developers.openai.com/codex/app-server#steer-an-active-turn.

This is not a claim that public Codex always cancels an in-flight HTTP request:
`core/tests/suite/pending_input.rs::user_input_does_not_preempt_after_reasoning_item`
explicitly preserves the original response and tool call. Memoh follows the
same run identity and safe tool boundaries, and additionally implements the
requested force-steer behavior during native model sampling. External drivers
and provider-specific Responses WebSocket steering are outside this mechanism.

## Follow-up

A follow-up is bound to the session. It records the run that was active when
it was enqueued (`EnqueuedDuringRunID`) but is consumed by whichever run
finishes next.

Two producers share the queue:

- The queue panel and the `/queue` slash command store `{"text": ...}`. The
  continuation starts as an ordinary chat turn on the same session. `/queue`
  is accepted only on local channel types (web, cli); a platform channel gets
  `queue_follow_up_unsupported_channel`, because it could not receive the
  reply of a run the server starts from the queue. `/steer` stays available on
  every channel: it joins the run whose reply the channel is already
  streaming.
- A complete turn that met a busy session (`turn.ErrSessionBusy`) may be
  stored through `EnqueueDeferredTurn` as `{"text": ..., "command": ...}` with
  the full `StartTurnCommand`. The continuation keeps the route, reply target,
  attachments, and metadata of the original message. The caller sees
  `turn.ErrTurnDeferred`; if the run ended before the enqueue, the caller sees
  `ErrQueueNoActiveRun` and retries ordinary admission.
  Only the local channel types (web, cli) do this: their users observe the
  resulting run through the session runtime subscription. Platform channels
  deliver replies by streaming the caller's run handle, and a run started from
  the queue has no such consumer, so they keep the bounded busy retry and
  surface `ErrSessionBusy` when it expires. `StartTurn` itself never defers.

Consumption:

- The terminal observer claims the oldest accepted follow-up for the finished
  run and starts it through normal turn admission with `NoDefer` set and the
  retry identity `follow-up:<item_id>`. Admission remains the only owner and
  fencing authority; the queue only selects payload.
- One starter runs per session at a time. A successful start applies the
  claim; a failed start releases it so the next terminal boundary claims it
  again. Ordinary user turns may win the session slot first; the follow-up
  then waits for that run to end.
- An enqueue that observed an active run re-checks the live snapshot after
  writing. If the run has already ended, the enqueue path starts the follow-up
  itself, so an item cannot wait for an unrelated later run.
- Follow-ups are not rejected when the run they were queued behind aborts,
  fails, or is lost. The queued input still belongs to the session; only
  steers, which are run-bound, are rejected at terminal.

## Redis transactions

Queue mutations run as `WATCH`/`MULTI` transactions whose watch set is the
queue document plus, where a decision depends on ownership, the run key that
the finishing owner deletes. The session state key is read inside the
transaction but never watched: it is rewritten on every streamed runtime delta
and watching it would fail queue transactions during normal output. A steer
admitted against a snapshot that turns terminal is still closed by
`CloseSteerRun`, which serializes on the queue document.

Conflicting transactions retry with exponential backoff up to eight times and
then return `ErrQueueAdmissionOverloaded`.

## PostgreSQL boundary

PostgreSQL remains authoritative for ordinary run admission, ownership and
fencing, history, user-input/approval state, and other durable application
records. It does not store queue payloads, queue claims, follow-up
continuation provenance, or queue step-commit records. The queue feature was
never added to the canonical schema or migration chain; deployments upgrade
directly from the existing `0145` schema.

## Recovery and availability

Because queues are live state, a process restart with the memory backend (or a
Redis data loss event) may leave no pending item to recover. This is an explicit
availability trade-off for low-latency input handling. Normal run/history
durability and fencing are unaffected. Clients should treat queue errors as
runtime availability errors and retry with a new invocation ID only when the
original result was not observed.
