import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useDisplayClipboard } from './useDisplayClipboard'

const errorToast = vi.hoisted(() => vi.fn())
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@felinic/ui', () => ({ toast: { error: errorToast } }))

const scopes: ReturnType<typeof effectScope>[] = []
afterEach(() => {
  scopes.splice(0).forEach(scope => scope.stop())
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

async function setup(state: PermissionState = 'prompt', focused = true, secure = true) {
  const permission = Object.assign(new EventTarget(), { state })
  const hasFocus = vi.fn(() => focused)
  const written: string[] = []
  const write = vi.fn(async (items: { data: Record<string, Promise<Blob>> }[]) => {
    written.push(await (await items[0]!.data['text/plain']!).text())
  })
  const writeText = vi.fn(async (text: string) => { written.push(text) })
  vi.stubGlobal('window', { isSecureContext: secure })
  vi.stubGlobal('document', { hasFocus })
  vi.stubGlobal('ClipboardItem', class {
    data: Record<string, Promise<Blob>>
    constructor(data: Record<string, Promise<Blob>>) { this.data = data }
  })
  vi.stubGlobal('navigator', {
    permissions: { query: vi.fn(async () => permission) },
    userActivation: { isActive: true }, clipboard: { write, writeText },
  })
  const connection = {
    clipboardText: ref(''), clipboardRevision: ref(0), inputChannel: ref(null),
    inputReady: () => true, pasteText: vi.fn(() => true),
  }
  const scope = effectScope()
  scopes.push(scope)
  const clipboard = scope.run(() => useDisplayClipboard(connection, () => true))!
  await Promise.resolve()
  const receive = (text: string) => {
    connection.clipboardText.value = text
    connection.clipboardRevision.value++
  }
  return { clipboard, permission, hasFocus, write, writeText, written, receive }
}

describe('display clipboard permission and gesture routing', () => {
  it('prepares menu copies with prompt permission and never writes stale text', async () => {
    const { clipboard, write, written, receive } = await setup()
    clipboard.preparePointerCopy()
    clipboard.preparePointerCopy(true) // mouseup must retain the mousedown request
    expect(write).toHaveBeenCalledTimes(1)
    expect(written).toEqual([])
    receive('remote selection')
    await vi.waitFor(() => expect(written).toEqual(['remote selection']))
  })

  it('can prepare on mouseup after the initial pointer gesture restored document focus', async () => {
    const { clipboard, hasFocus, write, receive, written } = await setup('prompt', false)
    clipboard.preparePointerCopy()
    expect(write).not.toHaveBeenCalled()
    hasFocus.mockReturnValue(true)
    clipboard.preparePointerCopy()
    receive('same text')
    await vi.waitFor(() => expect(written).toEqual(['same text']))
  })

  it('uses background permission when granted and updates after revocation', async () => {
    const { clipboard, permission, write, writeText, receive } = await setup('granted')
    clipboard.preparePointerCopy()
    expect(write).not.toHaveBeenCalled()
    receive('background copy')
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith('background copy'))
    permission.state = 'prompt'
    permission.dispatchEvent(new Event('change'))
    clipboard.preparePointerCopy()
    expect(write).toHaveBeenCalledTimes(1)
  })

  it('explains the secure-context requirement instead of suggesting focus', async () => {
    const { clipboard, write } = await setup('prompt', true, false)
    clipboard.prepareRemoteCopy()
    expect(errorToast).toHaveBeenCalledWith('chat.display.clipboard.copyRequiresSecureContext')
    expect(write).not.toHaveBeenCalled()
  })
})
