import { computed, inject, ref, watch } from 'vue'
import { DesktopUpdatesKey, type DesktopUpdateState } from '@/lib/desktop-shell'
import { mockDesktopUpdates } from '@/lib/desktop-updates-mock'

/** One bridge owns the snapshot and event stream, including dev mock swaps. */
export function useDesktopUpdates() {
  const realBridge = inject(DesktopUpdatesKey, undefined)
  const bridge = computed(() => import.meta.env.DEV && mockDesktopUpdates.enabled.value
    ? mockDesktopUpdates.bridge : realBridge)
  const state = ref<DesktopUpdateState | null>(null)
  watch(bridge, (current, _previous, onCleanup) => {
    state.value = null
    if (!current) return
    let active = true
    let receivedEvent = false
    const stop = current.onStateChanged((next) => {
      if (!active) return
      receivedEvent = true
      state.value = next
    })
    onCleanup(() => { active = false; stop() })
    void current.getState().then((next) => {
      if (active && !receivedEvent) state.value = next
    }).catch((error: unknown) => {
      if (active) console.warn('Failed to read desktop update state', error)
    })
  }, { immediate: true })
  return { bridge, state }
}
