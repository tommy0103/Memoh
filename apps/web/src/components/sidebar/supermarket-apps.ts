import type { HandlersAppItem, HandlersSupermarketAppSummary } from '@memohai/sdk'
import { appDisplayDescription, appDisplayName, appKey } from '@/composables/api/useApps'

export function filterInstalledApps(items: HandlersAppItem[], search: string, locale: string): HandlersAppItem[] {
  const query = search.trim().toLocaleLowerCase()
  return items.filter(item => [appDisplayName(item, locale), appDisplayDescription(item, locale), item.app_id]
    .some(value => value?.toLocaleLowerCase().includes(query)))
}

/** Existing and in-flight records stay in the installed section, including failed operations. */
export function uninstalledApps(catalog: HandlersSupermarketAppSummary[], installed: HandlersAppItem[]): HandlersSupermarketAppSummary[] {
  const keys = new Set(installed.map(appKey))
  return catalog.filter(app => !keys.has(appKey(app)))
}
