import { describe, expect, it } from 'vitest'
import { isRuntimeContinuationUserTurn, isRuntimeSteerTurnId, isRuntimeSteerUserTurn } from './types'

describe('runtime queue turn classification', () => {
  it('recognizes only a continuation user turn as a follow-up input', () => {
    expect(isRuntimeContinuationUserTurn({
      role: 'user',
      turnId: 'continuation-turn',
      runtimeRunId: 'continuation-run',
      runtimeContinuation: true,
    })).toBe(true)

    expect(isRuntimeContinuationUserTurn({
      role: 'user',
      turnId: 'queue-steer:item-1',
      runtimeRunId: 'run-1',
      runtimeContinuation: true,
      runtimeSteer: true,
    })).toBe(false)

    expect(isRuntimeContinuationUserTurn({
      role: 'user',
      turnId: 'ordinary-turn',
      runtimeRunId: 'run-1',
      runtimeContinuation: false,
    })).toBe(false)
  })

  // The server names a steer's turn when it claims the input, so a steer's turn
  // id is an ordinary one. Anything that classified a steer by the provisional
  // `queue-steer:` shape stopped matching the moment that landed; the marker
  // the frame sets is what keeps the classification working either way.
  it('recognizes a steer named at claim time as a steer', () => {
    expect(isRuntimeSteerUserTurn({ role: 'user', runtimeSteer: true })).toBe(true)
    expect(isRuntimeSteerUserTurn({ role: 'user' })).toBe(false)
    expect(isRuntimeSteerUserTurn({ role: 'assistant', runtimeSteer: true })).toBe(false)

    // A steer that the server named is excluded from the follow-up class even
    // though its turn id no longer carries the provisional prefix.
    expect(isRuntimeContinuationUserTurn({
      role: 'user',
      turnId: 'turn-6',
      runtimeRunId: 'run-1',
      runtimeContinuation: true,
      runtimeSteer: true,
    })).toBe(false)
  })

  it('keeps the existing steer identity separate', () => {
    expect(isRuntimeSteerTurnId('queue-steer:item-1')).toBe(true)
    expect(isRuntimeSteerTurnId('continuation-turn')).toBe(false)
  })
})
