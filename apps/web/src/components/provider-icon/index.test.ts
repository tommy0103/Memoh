// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, ref, shallowRef } from 'vue'

vi.mock('./icons.ts', () => ({ iconMap: {} }))
vi.mock('./preload', () => ({ providerIconSource: (url: string) => shallowRef(url.includes('/blocked') ? null : url) }))
import ProviderIcon from './index.vue'

let app: ReturnType<typeof createApp> | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = '' })

it('replaces a failed image with the caller fallback and recovers when its URL changes', async () => {
  const icon = ref('https://example.com/dead.svg')
  const root = document.createElement('div')
  document.body.append(root)
  app = createApp(() => h(ProviderIcon, { icon: icon.value, class: 'size-4' }, {
    default: () => h('svg', { 'data-fallback': '' }),
  }))
  app.mount(root)
  root.querySelector('img')!.dispatchEvent(new Event('error'))
  await nextTick()
  expect(root.querySelector('img')).toBeNull()
  expect(root.querySelector('span.size-4 > svg[data-fallback]')).not.toBeNull()
  icon.value = 'https://example.com/working.svg'
  await nextTick()
  expect(root.querySelector('img')!.getAttribute('src')).toBe(icon.value)
  expect(root.querySelector('[data-fallback]')).toBeNull()
})

it('shows the caller fallback without an image request for a native rejection', async () => {
  const root = document.createElement('div')
  document.body.append(root)
  app = createApp(() => h(ProviderIcon, { icon: 'https://example.com/blocked', class: 'size-4' }, {
    default: () => h('svg', { 'data-fallback': '' }),
  }))
  app.mount(root)
  expect(root.querySelector('img')).toBeNull()
  expect(root.querySelector('span.size-4 > svg[data-fallback]')).not.toBeNull()
})
