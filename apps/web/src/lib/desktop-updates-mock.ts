import { ref } from 'vue'
import type {
  DesktopUpdateBridge,
  DesktopUpdateState,
  DesktopUpdateStatus,
} from '@/lib/desktop-shell'

// DEV-only bridge shared by the footer and About; never invokes the native installer.
export interface MockDesktopUpdates {
  enabled: typeof enabled
  state: typeof state
  bridge: DesktopUpdateBridge
  arm(): void
  disarm(): void
  setStatus(status: DesktopUpdateStatus): void
  setProgress(percent: number): void
  setState(patch: Partial<DesktopUpdateState>): void
}

const enabled = ref(false)
const state = ref<DesktopUpdateState>({
  status: 'idle',
  autoUpdate: true,
  currentVersion: '0.0.0-mock',
  latestVersion: null,
  progress: null,
  error: null,
  releaseNotes: null,
})

const listeners = new Set<(state: DesktopUpdateState) => void>()

function emit() {
  for (const listener of listeners) listener(state.value)
}

function setState(patch: Partial<DesktopUpdateState>) {
  state.value = { ...state.value, ...patch }
  emit()
}

function setStatus(status: DesktopUpdateStatus) {
  switch (status) {
    case 'downloading':
      setState({ status, latestVersion: '9.9.9-mock', progress: state.value.progress ?? 42, error: null })
      break
    case 'downloaded':
      setState({
        status,
        latestVersion: '9.9.9-mock',
        progress: 100,
        error: null,
        releaseNotes: '### Improvements\n- Mock release notes for the update chip\n- Safe to ignore',
      })
      break
    case 'error':
      setState({ status, progress: null, error: 'Mock update failure' })
      break
    default:
      setState({ status, progress: null, error: null })
  }
}

const bridge: DesktopUpdateBridge = {
  getInfo: async () => ({ version: state.value.currentVersion, platform: 'mock', enabled: true }),
  getState: async () => state.value,
  check: async () => {
    setStatus('downloading')
    return state.value
  },
  setAutoUpdate: async (autoUpdate) => {
    setState({ autoUpdate })
    return state.value
  },
  install: async () => {
    if (state.value.status === 'downloaded') setStatus('installing')
    return state.value
  },
  onStateChanged: (listener) => {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },
}

export const mockDesktopUpdates: MockDesktopUpdates = {
  enabled,
  state,
  bridge,
  arm: () => { enabled.value = true },
  disarm: () => {
    enabled.value = false
    setState({ status: 'idle', progress: null, error: null })
  },
  setStatus,
  setProgress: (percent: number) => setState({ status: 'downloading', progress: percent }),
  setState,
}

if (import.meta.env.DEV) {
  ;(window as unknown as { __memohMockUpdate?: MockDesktopUpdates }).__memohMockUpdate = mockDesktopUpdates
}
