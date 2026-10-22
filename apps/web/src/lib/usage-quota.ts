import type { InjectionKey } from 'vue'

// Quota summary for the footer user menu's Usage row (the trailing "12%").
// The OSS server has no quota concept — the hosted distribution provides this
// bridge at its bootstrap; when the key is absent the menu renders a plain
// "Usage" item. null also means "no quota surface" (endpoint missing or the
// fetch failed): degrade silently, never toast, from a menu row.
export interface UsageQuotaBridge {
  getUsedPercent(): Promise<number | null>
}

export const UsageQuotaKey: InjectionKey<UsageQuotaBridge | undefined> = Symbol('memohai:usage-quota')
