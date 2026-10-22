import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { RuntimeCurrentRunView, UITurn } from '@/composables/api/useChat.types'
import { createBackgroundTaskTracker } from './background-tasks'
import { projectRuntimeTranscript } from './runtime-projection'
import { createTranscriptController } from './transcript'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

function runView(overrides: Partial<RuntimeCurrentRunView> = {}): RuntimeCurrentRunView {
  return {
    run_id: 'run-1',
    turn_id: 'turn-5',
    turn_position: 5,
    generation: 'generation-1',
    status: 'running',
    started_at: '2026-07-27T08:00:00.000Z',
    updated_at: '2026-07-27T08:00:05.000Z',
    messages: [{ id: 0, type: 'text', content: 'live reply' }],
    user_turns: [{
      turn_id: 'turn-5',
      turn_position: 5,
      role: 'user',
      text: 'live ask',
      timestamp: '2026-07-27T08:00:00.000Z',
    }],
    ...overrides,
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

function makeTranscript(fetchMessages = vi.fn().mockResolvedValue([])) {
  const currentBotId = ref<string | null>('bot-1')
  const sessionId = ref<string | null>('session-1')
  const backgroundTasks = createBackgroundTaskTracker()
  const transcript = createTranscriptController({
    currentBotId,
    sessionId,
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages,
    locateMessage: vi.fn(),
    isTurnLive: () => true,
  })
  return { transcript, fetchMessages }
}

describe('live turn positions', () => {
  it('numbers the request turn and its assistant segment from the run view', () => {
    const slice = projectRuntimeTranscript(runView())

    expect(slice.turnPosition).toBe(5)
    expect(slice.turns.map(turn => [turn.role, turn.turn_position]))
      .toEqual([['user', 5], ['assistant', 5]])
  })

  it('numbers a post-steer segment from the steer turn, not the run turn', () => {
    const slice = projectRuntimeTranscript(runView({
      messages: [
        { id: 0, type: 'text', content: 'before steer' },
        { id: 1, type: 'text', content: 'after steer' },
      ],
      user_turns: [
        { turn_id: 'turn-5', turn_position: 5, role: 'user', text: 'live ask', timestamp: '2026-07-27T08:00:00.000Z' },
        { turn_id: 'turn-6', turn_position: 6, role: 'user', text: 'steer me', timestamp: '2026-07-27T08:00:02.000Z' },
      ],
      steer_turns: [{
        item_id: 'item-1',
        status: 'applied',
        text: 'steer me',
        turn_id: 'turn-6',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    }))

    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'turn-6', 6],
      ['assistant', 'turn-6', 6],
    ])
  })

  it('leaves a claimed steer unnumbered until its step commits', () => {
    const slice = projectRuntimeTranscript(runView({
      steer_turns: [{
        item_id: 'item-1',
        status: 'claimed',
        text: 'steer me',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    }))

    // Both the provisional steer bubble and the segment named after it stay
    // unnumbered: they are the tail boundary until the step commit numbers them.
    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'queue-steer:item-1', undefined],
      ['assistant', 'queue-steer:item-1', undefined],
    ])
  })

  // The #1179-era regression: a channel turn persisted while a run streams has
  // a later position but an earlier timestamp than the live assistant segment.
  // Ordering by time puts the reply after a turn that came later.
  it('orders a live turn against a settled one by position, not timestamp', () => {
    const { transcript } = makeTranscript()
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))
    // The user scrolled up once, so refreshes take the sorting merge path.
    transcript.hasLoadedOlder.value = true

    transcript.mergeMessages([
      settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:03.000Z'),
      settledTurn('m7', 'turn-6', 6, 'assistant', '2026-07-27T08:00:04.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => [turn.role, turn.turnId])).toEqual([
      ['user', 'turn-5'],
      ['assistant', 'turn-5'],
      ['user', 'turn-6'],
      ['assistant', 'turn-6'],
    ])
  })

  it('numbers the optimistic pair from run_accepted', () => {
    const { transcript } = makeTranscript()
    const assistantTurn = transcript.createOptimisticAssistantTurn('invocation-1')
    const userTurn = transcript.createOptimisticUserTurn('hi', undefined, 'invocation-1')
    transcript.appendToView(userTurn, assistantTurn)

    transcript.bindRuntimeTurn('invocation-1', 'turn-5', 'run-1', 5)

    expect(transcript.messages.map(turn => turn.turnPosition)).toEqual([5, 5])
  })

  // A live turn now carries a position, so position can no longer stand in for
  // "the database can address this". Paging from one would send a render id as
  // the cursor.
  it('never pages from a live turn that has a position but no database row', async () => {
    const fetchMessages = vi.fn().mockResolvedValue([])
    const { transcript } = makeTranscript(fetchMessages)
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))
    fetchMessages.mockClear()

    const loaded = await transcript.loadOlderMessages()

    expect(loaded).toBe(0)
    expect(fetchMessages).not.toHaveBeenCalled()
  })

  it('pages from the oldest settled turn even when live turns precede it', async () => {
    const fetchMessages = vi.fn().mockResolvedValue([])
    const { transcript } = makeTranscript(fetchMessages)
    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'user', '2026-07-27T07:00:00.000Z'),
    ], 'session-1')
    fetchMessages.mockClear()

    await transcript.loadOlderMessages()

    expect(fetchMessages).toHaveBeenCalledWith('bot-1', 'session-1', {
      limit: 30,
      beforeMessageId: 'm1',
    })
  })
})

describe('turns the database has not numbered yet', () => {
  // Positions come from one monotonic per-session counter, so an unnumbered
  // turn will always be numbered after every turn that already is. Ordering it
  // by timestamp instead put a streaming steer above the request it answers:
  // the request row is timestamped at step commit, which is later than the
  // steer the model already accepted.
  it('sorts after every numbered turn instead of by timestamp', () => {
    const { transcript } = makeTranscript()
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView({
      messages: [
        { id: 0, type: 'text', content: 'before steer' },
        { id: 1, type: 'text', content: 'after steer' },
      ],
      user_turns: [{
        turn_id: 'turn-5',
        turn_position: 5,
        role: 'user',
        text: 'live ask',
        // Persisted at step commit, so later than the steer below it.
        timestamp: '2026-07-27T08:00:09.000Z',
      }],
      steer_turns: [{
        item_id: 'item-1',
        status: 'claimed',
        text: 'steer me',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    })))
    transcript.hasLoadedOlder.value = true

    transcript.mergeMessages([
      settledTurn('m1', 'turn-1', 1, 'user', '2026-07-27T07:00:00.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual([
      'user:turn-1',
      'user:turn-5',
      'assistant:turn-5',
      'user:queue-steer:item-1',
      'assistant:queue-steer:item-1',
    ])
  })

  // Both halves of an uncommitted steer carry the same provisional identity, so
  // role ordering inside the turn applies. Naming the segment separately left
  // the pair with no shared key and the reply rendered above its own request.
  it('keeps an uncommitted steer and its reply as one turn', () => {
    const slice = projectRuntimeTranscript(runView({
      messages: [{ id: 0, type: 'text', content: 'before' }, { id: 1, type: 'text', content: 'after' }],
      steer_turns: [{
        item_id: 'item-1',
        status: 'claimed',
        text: 'steer me',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    }))

    const provisional = slice.turns.filter(turn => turn.turn_id.startsWith('queue-steer:'))
    expect(provisional.map(turn => [turn.role, turn.turn_id, turn.id])).toEqual([
      ['user', 'queue-steer:item-1', 'runtime:queue-steer:item-1:user'],
      ['assistant', 'queue-steer:item-1', 'runtime:queue-steer:item-1:assistant'],
    ])
  })

  // A channel message persisted while a run streams takes a later position than
  // the run's own turn. Appending the retained live turn rendered the reply
  // below a request that came after it.
  it('places a retained live turn by position, not at the tail', () => {
    const { transcript } = makeTranscript()
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))

    transcript.replaceMessages([
      settledTurn('m5', 'turn-5', 5, 'user', '2026-07-27T08:00:00.000Z'),
      settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:03.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual([
      'user:turn-5',
      'assistant:turn-5',
      'user:turn-6',
    ])
  })
})

describe('runtime frames preserve interleaved history', () => {
  it.each([false, true])('keeps a channel turn ahead of the next steer across frames (persisted=%s)', (persisted) => {
    const { transcript } = makeTranscript()
    const run = runView({
      messages: [
        { id: 0, type: 'text', content: 'before steer' },
        { id: 1, type: 'text', content: 'after steer' },
      ],
      steer_turns: [{
        item_id: 'item-1',
        status: persisted ? 'applied' : 'claimed',
        ...(persisted ? { turn_id: 'turn-8' } : {}),
        text: 'steer me',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:08.000Z',
      }],
    })
    if (persisted) run.user_turns!.push({
      turn_id: 'turn-8', turn_position: 8, role: 'user', text: 'steer me',
      timestamp: '2026-07-27T08:00:08.000Z',
    })
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))
    transcript.mergeMessages([
      settledTurn('m5', 'turn-5', 5, 'user', '2026-07-27T08:00:05.000Z'),
      settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:06.000Z'),
    ], 'session-1')
    const steerId = persisted ? 'turn-8' : 'queue-steer:item-1'
    const expected = ['user:turn-5', 'assistant:turn-5', 'user:turn-6', `user:${steerId}`, `assistant:${steerId}`]
    const identities = transcript.messages.map(turn => turn.id)
    const channel = transcript.messages[2]
    expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual(expected)

    for (const id of [2, 3]) {
      run.messages.push({ id, type: 'text', content: `next block ${id}` })
      transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))
      expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual(expected)
      expect(transcript.messages.map(turn => turn.id)).toEqual(identities)
      expect(transcript.messages[2]).toBe(channel)
      const last = transcript.messages.at(-1)
      expect(last?.role === 'assistant' && last.messages.at(-1)).toMatchObject({ type: 'text', content: `next block ${id}` })
    }
  })

  it('places the initial runtime snapshot around unrelated history without sorting that history', () => {
    const { transcript } = makeTranscript()
    transcript.replaceMessages([
      settledTurn('z6', 'turn-6', 6, 'user', '2026-07-27T08:00:06.000Z'),
      settledTurn('a6', 'turn-other', 6, 'user', '2026-07-27T08:00:06.000Z'),
    ], 'session-1')

    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))

    expect(transcript.messages.map(turn => turn.id)).toEqual([
      'runtime:turn-5:user', 'runtime:turn-5:assistant', 'z6', 'a6',
    ])
  })

  // A local turn the server has not named yet will be numbered after everything
  // that already is, so a numbered runtime turn belongs ahead of it. Skipping
  // unnumbered entries while searching sent the run's own streaming reply below
  // a pending local bubble.
  it('places a numbered runtime turn ahead of an unnamed local turn', () => {
    const { transcript } = makeTranscript()
    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'user', '2026-07-27T07:00:00.000Z'),
    ], 'session-1')
    transcript.appendToView(
      transcript.createOptimisticUserTurn('not named yet', undefined, 'invocation-other'),
    )
    // Read back through the reactive array so the identity check compares the
    // same proxy the transcript holds.
    const pending = transcript.messages.at(-1)

    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))

    expect(transcript.messages.map(turn => turn.turnId ?? 'unnamed')).toEqual([
      'turn-1',
      'turn-5',
      'turn-5',
      'unnamed',
    ])
    expect(transcript.messages.at(-1)).toBe(pending)
  })

  it('keeps the existing anchor for older servers without positions', () => {
    const { transcript } = makeTranscript()
    const run = runView({ turn_position: undefined })
    run.user_turns![0]!.turn_position = undefined
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))
    transcript.appendToView(transcript.normalizeTurn(settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:06.000Z')))

    transcript.applyRuntimeTranscript(projectRuntimeTranscript(run))

    expect(transcript.messages.map(turn => `${turn.role}:${turn.turnId}`)).toEqual([
      'user:turn-5', 'assistant:turn-5', 'user:turn-6',
    ])
  })
})
