// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createApp, h, ref, type App } from 'vue'
import { createPinia } from 'pinia'
import { PiniaColada } from '@pinia/colada'
import AppDetail from './app-detail.vue'

const api = vi.hoisted(() => ({ app: vi.fn(), dependencies: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getSupermarketRegistriesByRegistryIdAppsByAppId: api.app,
  getSupermarketRegistriesByRegistryIdAppsByAppIdReleasesByRevision: api.app,
  getWorkspaceDependencies: api.dependencies,
  getSupermarketRegistries: async () => ({ data: { data: [] } }),
  getConnectorsCatalog: async () => ({ data: [] }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { registryId: 'memoh', appId: 'github' }, query: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('zh') }) }))
vi.mock('@/store/capabilities', () => ({ useCapabilitiesStore: () => ({ connectors: false, load: vi.fn() }) }))
vi.mock('@/lib/api-client', () => ({ sdkApiUrl: ({ url }: { url: string }) => url }))
vi.mock('@/utils/api-error', () => ({ resolveApiErrorMessage: (_: unknown, fallback: string) => fallback }))
vi.mock('@/composables/api/useApps', () => ({
  categoryDisplayName: () => '', appDisplayName: (app: { name: string }) => app.name,
  appDisplayDescription: () => '', useAppCategoriesQuery: () => ({ data: ref([]) }),
}))
vi.mock('./components/install-app-dialog.vue', () => ({ default: { render: () => null } }))
vi.mock('./components/market-detail-header.vue', () => ({ default: { render: () => null } }))
vi.mock('./components/skill-icon.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/provider-icon/index.vue', () => ({ default: { render: () => null } }))
vi.mock('@felinic/ui', () => {
  const slot = { template: '<div><slot name="actions"/><slot name="leading"/><slot name="content"/><slot/></div>' }
  return { Badge: slot, Button: { template: '<button><slot/></button>' }, DetailPane: slot,
    InlineLoadingRow: slot, SettingsRow: slot, SettingsSection: slot, SettingsShell: slot, toast: { error: vi.fn() } }
})
let app: App
let root: HTMLDivElement
beforeEach(() => {
  api.app.mockResolvedValue({ data: { name: 'GitHub', dependencies: ['git'], connectors: [], skills: [], tags: [], revision: 'a'.repeat(64) } })
  root = document.createElement('div')
  document.body.append(root)
})
afterEach(() => { app?.unmount(); root.remove(); vi.resetAllMocks() })
function mount() {
  app = createApp({ render: () => h(AppDetail) })
  app.config.globalProperties.$t = (key: string) => key
  app.use(createPinia()).use(PiniaColada)
  app.mount(root)
}
it('renders localized dependency metadata and its own icon without querying a same-name App', async () => {
  const icon = `/workspace-dependencies/icons/${'a'.repeat(64)}`
  api.dependencies.mockResolvedValue({ data: { items: [{ id: 'git', name: 'Git', description: 'Git tools', icon_url: icon, translations: { zh: { name: 'Git 工具', description: '管理仓库' } } }] } })
  mount()
  await vi.waitFor(() => expect(root.textContent).toContain('管理仓库'))
  expect(root.textContent).toContain('Git 工具')
  expect(root.querySelector('img')?.getAttribute('src')).toBe(icon)
  expect(api.app).toHaveBeenCalledTimes(1)
  expect(api.app.mock.calls[0]?.[0].path.app_id).toBe('github')
})
it('ends loading on failure and retries the dependency query', async () => {
  api.dependencies.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ data: { items: [] } })
  mount()
  await vi.waitFor(() => expect(root.textContent).toContain('supermarket.dependenciesLoadError'))
  expect(root.textContent).not.toContain('supermarket.dependencyPending')
  const retry = [...root.querySelectorAll('button')].find(button => button.textContent === 'common.retry')
  expect(retry).toBeTruthy()
  retry!.click()
  await vi.waitFor(() => expect(root.textContent).toContain('supermarket.dependencyUnavailable'))
  expect(root.textContent).not.toContain('supermarket.dependencyPending')
})
