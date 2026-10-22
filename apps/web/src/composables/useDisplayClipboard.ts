import { onScopeDispose, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from '@felinic/ui'
import type { BotDisplayConnection } from './useBotDisplayConnection'

type ClipboardConnection = Pick<BotDisplayConnection,
  'clipboardText' | 'clipboardRevision' | 'inputChannel' | 'inputReady' | 'pasteText'>

export function useDisplayClipboard(connection: ClipboardConnection, isActive: () => boolean) {
  const { t } = useI18n()
  let pending: { pointer: boolean; received: boolean; resolve: (text: Blob) => void; cancel: () => void } | null = null
  let contextRevision = 0
  let clipboardRevision = 0
  let pointerCopyEnabled = true
  let permissionCleanup: (() => void) | undefined
  let disposed = false

  // Only a granted background write can replace the gesture path. Chrome's
  // prompt state needs the same gesture as browsers without permission queries.
  async function detectGestureWrite() {
    if (!navigator.permissions?.query) return
    try {
      const descriptor = { name: 'clipboard-write' as PermissionName, allowWithoutGesture: true }
      const permission = await navigator.permissions.query(descriptor)
      if (disposed) return
      const update = () => { pointerCopyEnabled = permission.state !== 'granted' }
      update()
      permission.addEventListener('change', update)
      permissionCleanup = () => permission.removeEventListener('change', update)
    } catch {
      // Safari and Firefox reject this descriptor; keep the gesture path.
    }
  }
  void detectGestureWrite()

  function cancelCopy() {
    pending?.cancel()
    pending = null
  }

  // Start the write inside the input gesture. Safari can then wait for the
  // remote copy to arrive without losing the gesture's clipboard permission.
  function prepareRemoteCopy(pointer = false) {
    if (!isActive() || !connection.inputReady() || !document.hasFocus()) return
    // Do not replace a shortcut's explicit copy, or a copy already being written.
    if (pointer && pending && (!pending.pointer || pending.received)) return
    clipboardRevision++
    cancelCopy()
    const unavailable = !window.isSecureContext ? 'copyRequiresSecureContext'
      : !navigator.clipboard?.write || typeof ClipboardItem === 'undefined' ? 'copyUnsupported' : null
    if (unavailable) {
      if (!pointer) toast.error(t(`chat.display.clipboard.${unavailable}`))
      return
    }
    let resolve!: (text: Blob) => void
    let reject!: (reason: Error) => void
    const data = new Promise<Blob>((yes, no) => { resolve = yes; reject = no })
    // A copy with no selection may not produce a new remote value. Never
    // replace the clipboard with an empty or previously copied fallback.
    const timeout = setTimeout(() => {
      if (pending !== request) return
      const received = request.received
      cancelCopy()
      // Ctrl+C can interrupt a terminal process or have no selection to copy.
      if (received) toast.error(t('chat.display.clipboard.copyDenied'))
    }, 5000)
    const request = {
      pointer,
      received: false,
      resolve: (text: Blob) => { resolve(text) },
      cancel: () => { clearTimeout(timeout); reject(new Error('Copy gesture expired')) },
    }
    pending = request
    // A browser may reject before consuming the ClipboardItem promise.
    void data.catch(() => {})
    try {
      void navigator.clipboard.write([new ClipboardItem({ 'text/plain': data })]).then(() => {
        if (pending !== request) return
        clearTimeout(timeout)
        pending = null
      }).catch(() => {
        if (pending !== request) return
        cancelCopy()
        if (!pointer || request.received) toast.error(t('chat.display.clipboard.copyDenied'))
      })
    } catch {
      cancelCopy()
      if (!pointer) toast.error(t('chat.display.clipboard.copyDenied'))
    }
  }

  function preparePointerCopy(continuing = false) {
    // mousedown and mouseup share one request. The latter can be the first
    // focused gesture when Safari returns from its browser toolbar.
    if (continuing && pending) return
    if (pointerCopyEnabled && navigator.userActivation?.isActive !== false) prepareRemoteCopy(true)
  }

  function cancelPointerCopy() {
    if (pending?.pointer) cancelCopy()
  }

  watch(connection.clipboardRevision, (revision) => {
    if (!revision || !isActive() || !connection.inputReady()) return
    clipboardRevision++
    const text = connection.clipboardText.value
    if (pending?.pointer && !document.hasFocus()) cancelCopy()
    if (pending && !pending.received) {
      pending.received = true
      pending.resolve(new Blob([text], { type: 'text/plain' }))
      return
    }
    void syncRemoteClipboard(text)
  }, { flush: 'sync' })

  async function syncRemoteClipboard(text: string) {
    const channel = connection.inputChannel.value
    const context = contextRevision
    const revision = clipboardRevision
    const current = () => isActive() && document.hasFocus() && connection.inputReady()
      && channel === connection.inputChannel.value && context === contextRevision && revision === clipboardRevision && !pending
    try {
      // A granted write permission supports menu/application copies without a
      // key gesture. Safari has no such grant; keep its gesture write above.
      if (!navigator.permissions?.query || !current()) return
      const descriptor = { name: 'clipboard-write' as PermissionName, allowWithoutGesture: true }
      const permission = await navigator.permissions.query(descriptor)
      if (permission.state !== 'granted' || !current()) return
      await navigator.clipboard.writeText(text)
    } catch {
      // Unsupported/background writes must not prompt or overwrite a newer copy.
    }
  }

  function pasteText(text: string, shift = false) {
    clipboardRevision++
    cancelCopy()
    if (!isActive() || !connection.inputReady()) return
    if (!connection.pasteText(text, shift)) {
      toast.error(t('chat.display.clipboard.pasteFailed'))
      return
    }
  }

  watch([isActive, connection.inputChannel], () => {
    contextRevision++
    cancelCopy()
  }, { flush: 'sync' })
  onScopeDispose(() => {
    disposed = true
    permissionCleanup?.()
    contextRevision++
    cancelCopy()
  })
  return { prepareRemoteCopy, preparePointerCopy, cancelPointerCopy, pasteText }
}
