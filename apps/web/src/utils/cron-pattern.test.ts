import { describe, expect, it } from 'vitest'
import {
  defaultScheduleFormState,
  describeCron,
  fromCron,
  isValidCron,
  nextRuns,
  toCron,
  type ScheduleFormState,
} from './cron-pattern'

function mk(overrides: Partial<ScheduleFormState>): ScheduleFormState {
  return { ...defaultScheduleFormState(), ...overrides }
}

describe('toCron', () => {
  it('minutes mode', () => {
    expect(toCron(mk({ mode: 'minutes', intervalMinutes: 15 }))).toBe('*/15 * * * *')
  })

  it('hourly mode', () => {
    expect(toCron(mk({ mode: 'hourly', minute: 30 }))).toBe('30 * * * *')
  })

  it('daily mode single hour', () => {
    expect(toCron(mk({ mode: 'daily', minute: 0, hours: [9] }))).toBe('0 9 * * *')
  })

  it('daily mode multi hour gets sorted & deduped', () => {
    expect(toCron(mk({ mode: 'daily', minute: 30, hours: [18, 9, 9, 13] })))
      .toBe('30 9,13,18 * * *')
  })

  it('weekly mode', () => {
    expect(toCron(mk({
      mode: 'weekly',
      minute: 0,
      hours: [9],
      weekdays: [1, 3, 5],
    }))).toBe('0 9 * * 1,3,5')
  })

  it('monthly mode', () => {
    expect(toCron(mk({
      mode: 'monthly',
      minute: 0,
      hours: [9],
      monthDays: [1, 15],
    }))).toBe('0 9 1,15 * *')
  })

  it('yearly mode', () => {
    expect(toCron(mk({
      mode: 'yearly',
      minute: 0,
      hours: [12],
      month: 12,
      monthDay: 25,
    }))).toBe('0 12 25 12 *')
  })

  it('advanced mode passes through trimmed', () => {
    expect(toCron(mk({ mode: 'advanced', advancedPattern: '  @daily  ' }))).toBe('@daily')
  })

  it('rejects out-of-range values', () => {
    expect(() => toCron(mk({ mode: 'minutes', intervalMinutes: 60 }))).toThrow()
    expect(() => toCron(mk({ mode: 'hourly', minute: 60 }))).toThrow()
    expect(() => toCron(mk({ mode: 'daily', minute: 0, hours: [24] }))).toThrow()
  })
})

describe('fromCron', () => {
  it('recognises minutes mode', () => {
    const s = fromCron('*/15 * * * *')
    expect(s.mode).toBe('minutes')
    expect(s.intervalMinutes).toBe(15)
  })

  it('recognises hourly mode', () => {
    const s = fromCron('30 * * * *')
    expect(s.mode).toBe('hourly')
    expect(s.minute).toBe(30)
  })

  it('recognises daily mode with multiple hours', () => {
    const s = fromCron('30 9,13,18 * * *')
    expect(s.mode).toBe('daily')
    expect(s.minute).toBe(30)
    expect(s.hours).toEqual([9, 13, 18])
  })

  it('recognises weekly mode', () => {
    const s = fromCron('0 9 * * 1,3,5')
    expect(s.mode).toBe('weekly')
    expect(s.minute).toBe(0)
    expect(s.hours).toEqual([9])
    expect(s.weekdays).toEqual([1, 3, 5])
  })

  it('recognises monthly mode', () => {
    const s = fromCron('0 9 1,15 * *')
    expect(s.mode).toBe('monthly')
    expect(s.monthDays).toEqual([1, 15])
    expect(s.hours).toEqual([9])
  })

  it('recognises yearly mode', () => {
    const s = fromCron('0 12 25 12 *')
    expect(s.mode).toBe('yearly')
    expect(s.month).toBe(12)
    expect(s.monthDay).toBe(25)
    expect(s.hours).toEqual([12])
    expect(s.minute).toBe(0)
  })

  it('falls back to advanced for descriptors', () => {
    const s = fromCron('@daily')
    expect(s.mode).toBe('advanced')
    expect(s.advancedPattern).toBe('@daily')
  })

  it('falls back to advanced for 6-field cron', () => {
    const s = fromCron('0 */5 * * * *')
    expect(s.mode).toBe('advanced')
    expect(s.advancedPattern).toBe('0 */5 * * * *')
  })

  it('falls back to advanced for range expressions', () => {
    const s = fromCron('30 9 1-15 * *')
    expect(s.mode).toBe('advanced')
    expect(s.advancedPattern).toBe('30 9 1-15 * *')
  })

  it('falls back to advanced for step in hour field', () => {
    const s = fromCron('0 */2 * * *')
    expect(s.mode).toBe('advanced')
  })

  it('falls back to advanced for named weekdays', () => {
    const s = fromCron('0 9 * * MON-FRI')
    expect(s.mode).toBe('advanced')
  })

  it('falls back to advanced for empty input', () => {
    const s = fromCron('   ')
    expect(s.mode).toBe('advanced')
    expect(s.advancedPattern).toBe('')
  })
})

describe('round-trip fromCron(toCron(state))', () => {
  const cases: Array<{ label: string, state: ScheduleFormState }> = [
    { label: 'minutes', state: mk({ mode: 'minutes', intervalMinutes: 5 }) },
    { label: 'hourly', state: mk({ mode: 'hourly', minute: 45 }) },
    { label: 'daily single', state: mk({ mode: 'daily', minute: 0, hours: [9] }) },
    { label: 'daily multi', state: mk({ mode: 'daily', minute: 30, hours: [9, 13, 18] }) },
    { label: 'weekly', state: mk({ mode: 'weekly', minute: 0, hours: [9], weekdays: [1, 3, 5] }) },
    { label: 'monthly', state: mk({ mode: 'monthly', minute: 0, hours: [9], monthDays: [1, 15] }) },
    { label: 'yearly', state: mk({ mode: 'yearly', minute: 0, hours: [12], month: 12, monthDay: 25 }) },
  ]

  for (const { label, state } of cases) {
    it(label, () => {
      const pattern = toCron(state)
      const parsed = fromCron(pattern)
      expect(parsed.mode).toBe(state.mode)
      // Re-emit and compare canonical strings to confirm no drift.
      expect(toCron(parsed)).toBe(pattern)
    })
  }
})

describe('describeCron', () => {
  it('returns an english description', () => {
    const out = describeCron('0 9 * * *', 'en')
    expect(out).toBeTruthy()
    expect(out!.toLowerCase()).toContain('9')
  })

  it('returns a chinese description', () => {
    const out = describeCron('0 9 * * *', 'zh')
    expect(out).toBeTruthy()
  })

  it('returns undefined for invalid', () => {
    expect(describeCron('not a cron', 'en')).toBeUndefined()
  })
})

describe('nextRuns', () => {
  it('returns requested number of dates for valid pattern', () => {
    const runs = nextRuns('0 9 * * *', 'UTC', 3)
    expect(runs).toHaveLength(3)
    for (const d of runs) {
      expect(d.getUTCHours()).toBe(9)
    }
  })

  it('returns empty for invalid pattern', () => {
    expect(nextRuns('not valid', 'UTC', 3)).toEqual([])
  })
})

describe('isValidCron', () => {
  it('accepts 5-field cron', () => {
    expect(isValidCron('0 9 * * *')).toBe(true)
  })

  it('accepts descriptors', () => {
    expect(isValidCron('@daily')).toBe(true)
  })

  it('rejects garbage', () => {
    expect(isValidCron('')).toBe(false)
    expect(isValidCron('hello world')).toBe(false)
  })
})
