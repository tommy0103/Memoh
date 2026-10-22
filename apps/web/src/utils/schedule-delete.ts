import type { ScheduleSchedule } from '@memohai/sdk'

/**
 * The i18n key for the delete-schedule confirmation. A schedule that opens a
 * fresh session per run gets the warning that those sessions stop showing in
 * Schedule (its card is gone) and stay out of Recents, so only search finds
 * them afterwards. A schedule that runs in an existing conversation keeps the
 * plain confirmation: that conversation stays a normal Recents row. A missing
 * run target means new_session, the server's default.
 */
export function scheduleDeleteConfirmKey(schedule: Pick<ScheduleSchedule, 'run_target'> | null | undefined): string {
  return schedule?.run_target === 'existing_session'
    ? 'bots.schedule.deleteConfirm'
    : 'bots.schedule.deleteConfirmRunSessions'
}
