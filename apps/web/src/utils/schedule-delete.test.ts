import { describe, expect, it } from 'vitest'
import { scheduleDeleteConfirmKey } from './schedule-delete'

describe('scheduleDeleteConfirmKey', () => {
  it('warns that run sessions become search-only for a schedule that opens a session per run', () => {
    expect(scheduleDeleteConfirmKey({ run_target: 'new_session' })).toBe('bots.schedule.deleteConfirmRunSessions')
  })

  it('treats a schedule without a run target as opening a session per run, the server default', () => {
    expect(scheduleDeleteConfirmKey({})).toBe('bots.schedule.deleteConfirmRunSessions')
    expect(scheduleDeleteConfirmKey(null)).toBe('bots.schedule.deleteConfirmRunSessions')
  })

  it('keeps the plain confirmation for a schedule that runs in an existing conversation', () => {
    expect(scheduleDeleteConfirmKey({ run_target: 'existing_session' })).toBe('bots.schedule.deleteConfirm')
  })
})
