import {
  getBotsByBotIdScheduleByIdSessions,
  type ScheduleRunSession,
} from '@memohai/sdk'

// One fire of a schedule and the session it ran in. session_id is empty when
// the fire failed before any session existed; completed_at is null while the
// fire is still running.
export type { ScheduleRunSession }

export interface ScheduleRunSessionsPage {
  items: ScheduleRunSession[]
  /** Opaque cursor for the next older page, null when there is none. */
  nextCursor: string | null
}

/** Lists a schedule's runs newest first, each with the session it created. */
export async function fetchScheduleRunSessions(
  botId: string,
  scheduleId: string,
  options: { limit?: number, cursor?: string } = {},
): Promise<ScheduleRunSessionsPage> {
  const { data } = await getBotsByBotIdScheduleByIdSessions({
    path: { bot_id: botId, id: scheduleId },
    query: {
      limit: options.limit,
      ...(options.cursor ? { cursor: options.cursor } : {}),
    },
    throwOnError: true,
  })
  return {
    items: data.items ?? [],
    nextCursor: data.next_cursor?.trim() || null,
  }
}
