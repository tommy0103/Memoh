import { describe, expect, it } from 'vitest'
import type { HandlersSupermarketAppSummary } from '@memohai/sdk'
import { appKey } from '@/composables/api/useApps'
import { filterInstalledApps, uninstalledApps } from './supermarket-apps'

describe('sidebar supermarket lists', () => {
  it('keeps apps with the same ID in different registries distinct', () => {
    const catalog = [{ registry_id: 'memoh', app_id: 'python' }, { registry_id: 'other', app_id: 'python' }] as HandlersSupermarketAppSummary[]
    expect(uninstalledApps(catalog, [{ registry_id: 'memoh', app_id: 'python', status: 'installing' }])).toEqual([catalog[1]])
    expect(appKey(catalog[0]!)).toBe('memoh/python')
  })

  it('does not offer another install for a failed or discovered record', () => {
    const catalog = [{ registry_id: 'memoh', app_id: 'python' }] as HandlersSupermarketAppSummary[]
    for (const status of ['failed', 'discovered', 'removing', 'installed'] as const) {
      expect(uninstalledApps(catalog, [{ registry_id: 'memoh', app_id: 'python', status }])).toEqual([])
    }
  })

  it('searches localized names, descriptions and IDs without case or surrounding whitespace', () => {
    const items = [{ app_id: 'python', name: 'Python', translations: { zh: { name: '开发工具', description: '运行脚本' } } }]
    expect(filterInstalledApps(items, ' PYTHON ', 'zh')).toEqual(items)
    expect(filterInstalledApps(items, '脚本', 'zh-CN')).toEqual(items)
    expect(filterInstalledApps(items, '开发', 'zh')).toEqual(items)
    expect(filterInstalledApps(items, 'missing', 'en')).toEqual([])
    expect(filterInstalledApps(items, '', 'en')).toEqual(items)
  })
})
