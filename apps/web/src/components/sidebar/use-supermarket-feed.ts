import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { getSupermarketApps, type HandlersSupermarketAppSummary } from '@memohai/sdk'
import { appKey } from '@/composables/api/useApps'

/** Append catalog pages while isolating each search and retaining results on retryable failures. */
export function useSupermarketFeed(query: Ref<string>) {
  const items = shallowRef<HandlersSupermarketAppSummary[]>([])
  const loading = ref(false)
  const error = shallowRef<unknown>(null)
  const hasMore = ref(true)
  const page = ref(0)
  let generation = 0
  let controller: AbortController | undefined

  /** Automatic triggers stop after an error; only an explicit retry may resume that page. */
  async function loadMore() {
    if (loading.value || error.value || !hasMore.value) return
    const current = generation
    const nextPage = page.value + 1
    controller = new AbortController()
    loading.value = true
    try {
      const { data } = await getSupermarketApps({
        query: { q: query.value, page: nextPage, limit: 30, sort: 'relevance' },
        signal: controller.signal,
        throwOnError: true,
      })
      if (current !== generation) return
      const merged = new Map(items.value.map(item => [appKey(item), item]))
      for (const item of data.data) merged.set(appKey(item), item)
      items.value = [...merged.values()]
      page.value = nextPage
      hasMore.value = data.data.length > 0 && nextPage * (data.limit || 30) < data.total
    } catch (cause) {
      if (current === generation) error.value = cause
    } finally {
      if (current === generation) loading.value = false
    }
  }

  async function retry() {
    error.value = null
    await loadMore()
  }

  watch(query, () => {
    generation++
    controller?.abort()
    items.value = []
    page.value = 0
    error.value = null
    loading.value = false
    hasMore.value = true
    void loadMore()
  }, { immediate: true, flush: 'sync' })

  onScopeDispose(() => {
    generation++
    controller?.abort()
  })

  return { items, loading, error, hasMore, page, loadMore, retry }
}
