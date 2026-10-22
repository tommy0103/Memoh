import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { RuntimeCurrentRunView, UITurn } from '@/composables/api/useChat.types'
import { createBackgroundTaskTracker } from './background-tasks'
import { isRuntimeRunStreaming, projectRuntimeTranscript, runOwnsTurn } from './runtime-projection'
import { isRuntimeSteerUserTurn } from './types'
import { createTranscriptController } from './transcript'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

function runWithSteer(steer: Partial<RuntimeCurrentRunView['steer_turns'] extends (infer T)[] | undefined ? T : never>): RuntimeCurrentRunView {
  return {
    run_id: 'run-1',
    turn_id: 'turn-5',
    turn_position: 5,
    generation: 'generation-1',
    status: 'running',
    started_at: '2026-07-27T08:00:00.000Z',
    updated_at: '2026-07-27T08:00:05.000Z',
    messages: [
      { id: 0, type: 'text', content: 'before steer' },
      { id: 1, type: 'text', content: 'after steer' },
    ],
    user_turns: [{
      turn_id: 'turn-5',
      turn_position: 5,
      role: 'user',
      text: 'live ask',
      timestamp: '2026-07-27T08:00:00.000Z',
    }],
    steer_turns: [{
      item_id: 'item-1',
      status: 'claimed',
      text: 'steer me',
      after_message_id: 0,
      timestamp: '2026-07-27T08:00:02.000Z',
      ...steer,
    }],
  }
}

function settledTurn(
  id: string,
  turnId: string,
  position: number,
  role: 'user' | 'assistant',
  timestamp: string,
): UITurn {
  return role === 'user'
    ? { id, turn_id: turnId, turn_position: position, role, text: `u-${id}`, timestamp, platform: 'local' }
    : { id, turn_id: turnId, turn_position: position, role, messages: [], timestamp }
}

function makeTranscript(run: RuntimeCurrentRunView) {
  const backgroundTasks = createBackgroundTaskTracker()
  return createTranscriptController({
    currentBotId: ref<string | null>('bot-1'),
    sessionId: ref<string | null>('session-1'),
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages: vi.fn().mockResolvedValue([]),
    locateMessage: vi.fn(),
    isTurnLive: (_sessionId, turnId) =>
      Boolean(isRuntimeRunStreaming(run) && runOwnsTurn(run, turnId)),
  })
}

describe('a steer is named when it is claimed', () => {
  it('uses the server name and slot from the first claimed frame', () => {
    const slice = projectRuntimeTranscript(runWithSteer({ turn_id: 'turn-6', turn_position: 6 }))

    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'turn-6', 6],
      ['assistant', 'turn-6', 6],
    ])
  })

  // The reported regression: the step commit lands in history and its
  // session_touched refreshes the page before the runtime republishes the
  // steer. With the name drawn at claim time there is nothing to disagree
  // about — the settled row and the live bubble are the same turn.
  it('does not render a second bubble when history commits first', () => {
    const run = runWithSteer({ turn_id: 'turn-6', turn_position: 6 })
    const transcript = makeTranscript(run)
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))

    transcript.replaceMessages([
      settledTurn('m5', 'turn-5', 5, 'user', '2026-07-27T08:00:00.000Z'),
      settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:02.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual([
      'user:turn-5',
      'assistant:turn-5',
      'user:turn-6',
      'assistant:turn-6',
    ])
  })

  // A server that predates the claim-time naming still works: the bubble falls
  // back to the queue item identity, exactly as before.
  it('falls back to the queue item when the server sends no name', () => {
    const slice = projectRuntimeTranscript(runWithSteer({}))

    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'queue-steer:item-1', undefined],
      ['assistant', 'queue-steer:item-1', undefined],
    ])
  })
})

// Naming a steer at claim time removes the `queue-steer:` shape its turn id
// used to have, and scroll anchoring recognised a steer by exactly that shape
// (useQueueTurnAnchors) — so the anchor stopped firing with nothing to catch
// it. Drive the real producer: a hand-built slice would have kept passing.
describe('a named steer is still recognisable as a queue input', () => {
  it.each([
    ['named at claim time', { turn_id: 'turn-6', turn_position: 6 }],
    ['unnamed by an older server', {}],
  ])('marks the steer user turn (%s)', (_label, steer) => {
    const run = runWithSteer(steer)
    const transcript = makeTranscript(run)
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))

    const users = transcript.messages.filter(turn => turn.role === 'user')
    expect(users.map(turn => isRuntimeSteerUserTurn(turn))).toEqual([false, true])
  })
})
