import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@memohai/sdk', async importOriginal => ({
  ...await importOriginal<object>(),
  getBotsByBotIdSessions: vi.fn(),
}))

import { getBotsByBotIdSessions } from '@memohai/sdk'
import { fetchSessions } from './useChat.chat-api'

const listSessions = vi.mocked(getBotsByBotIdSessions)

describe('fetchSessions', () => {
  beforeEach(() => {
    listSessions.mockReset()
    listSessions.mockResolvedValue({ data: { items: [] } } as never)
  })

  it('leaves out schedule run sessions, which the schedule panel lists instead', async () => {
    await fetchSessions('bot-1')
    expect(listSessions.mock.calls[0]![0]!.query).toMatchObject({ hide_schedule_runs: true })
  })

  it('keeps the same rule for a workdir folder page', async () => {
    await fetchSessions('bot-1', { workdirId: 'wd-1', cursor: 'c1' })
    expect(listSessions.mock.calls[0]![0]!.query).toMatchObject({
      workdir_id: 'wd-1',
      cursor: 'c1',
      hide_schedule_runs: true,
    })
  })
})
