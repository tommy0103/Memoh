import { afterEach, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { useSupermarketFeed } from './use-supermarket-feed'

const fetchPage = vi.hoisted(() => vi.fn())
vi.mock('@memohai/sdk', () => ({ getSupermarketApps: fetchPage }))
const scopes: ReturnType<typeof effectScope>[] = []
afterEach(() => { scopes.splice(0).forEach(scope => scope.stop()); vi.resetAllMocks() })

function start() {
  const query = ref('')
  const scope = effectScope()
  scopes.push(scope)
  const feed = scope.run(() => useSupermarketFeed(query))!
  return { query, ...feed }
}
function page(ids: string[], total = 90) {
  return { data: { data: ids.map(app_id => ({ app_id, registry_id: 'memoh' })), total, limit: 30 } }
}
async function settle() { await nextTick(); await nextTick() }

it('appends pages, deduplicates identities, and stops at the server total', async () => {
  fetchPage.mockResolvedValueOnce(page(['a', 'b'])).mockResolvedValueOnce(page(['b', 'c'], 60))
  const feed = start()
  await settle()
  await feed.loadMore()
  expect(feed.items.value.map(app => app.app_id)).toEqual(['a', 'b', 'c'])
  expect(fetchPage.mock.calls[1]![0].query.page).toBe(2)
  expect(feed.hasMore.value).toBe(false)
  await feed.loadMore()
  expect(fetchPage).toHaveBeenCalledTimes(2)
})
it('coalesces concurrent triggers and stops on an empty page even with a stale total', async () => {
  let resolve!: (value: ReturnType<typeof page>) => void
  fetchPage.mockReturnValue(new Promise(r => { resolve = r }))
  const feed = start()
  void feed.loadMore()
  void feed.loadMore()
  expect(fetchPage).toHaveBeenCalledOnce()
  resolve(page([]))
  await settle()
  expect(feed.hasMore.value).toBe(false)
})
it('retains rows on failure and only retries the failed page explicitly', async () => {
  fetchPage.mockResolvedValueOnce(page(['a'])).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(page(['b'], 60))
  const feed = start()
  await settle()
  await feed.loadMore()
  expect(feed.items.value.map(app => app.app_id)).toEqual(['a'])
  expect(feed.error.value).toBeTruthy()
  await feed.loadMore()
  expect(fetchPage).toHaveBeenCalledTimes(2)
  await feed.retry()
  expect(fetchPage.mock.calls[2]![0].query.page).toBe(2)
  expect(feed.items.value.map(app => app.app_id)).toEqual(['a', 'b'])
  expect(feed.error.value).toBeNull()
})
it('discards stale search responses and aborts requests on disposal', async () => {
  let resolveOld!: (value: ReturnType<typeof page>) => void
  fetchPage.mockReturnValueOnce(new Promise(r => { resolveOld = r })).mockResolvedValueOnce(page(['new'], 1))
  const feed = start()
  const oldSignal = fetchPage.mock.calls[0]![0].signal
  feed.query.value = 'new'
  await settle()
  expect(oldSignal.aborted).toBe(true)
  resolveOld(page(['old']))
  await settle()
  expect(feed.items.value.map(app => app.app_id)).toEqual(['new'])
  const signal = fetchPage.mock.calls[1]![0].signal
  scopes[0]!.stop()
  expect(signal.aborted).toBe(true)
})

it('retries an initial failure and resets pagination for a new search', async () => {
  fetchPage.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(page(['a'])).mockResolvedValueOnce(page([], 0))
  const feed = start()
  await settle()
  expect(feed.error.value).toBeTruthy()
  await feed.retry()
  expect(fetchPage.mock.calls[1]![0].query.page).toBe(1)
  feed.query.value = 'missing'
  await settle()
  expect(fetchPage.mock.calls[2]![0].query).toMatchObject({ page: 1, q: 'missing' })
  expect(feed.items.value).toEqual([])
  expect(feed.hasMore.value).toBe(false)
})

it('ignores an old failure without releasing the new search loading lock', async () => {
  let rejectOld!: (reason: Error) => void
  let resolveNew!: (value: ReturnType<typeof page>) => void
  fetchPage.mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
    .mockReturnValueOnce(new Promise(resolve => { resolveNew = resolve }))
  const feed = start()
  feed.query.value = 'new'
  rejectOld(new Error('old request'))
  await settle()
  expect(feed.loading.value).toBe(true)
  expect(feed.error.value).toBeNull()
  await feed.loadMore()
  expect(fetchPage).toHaveBeenCalledTimes(2)
  resolveNew(page(['new'], 1))
  await settle()
  expect(feed.items.value.map(app => app.app_id)).toEqual(['new'])
})
