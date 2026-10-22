type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey' | 'repeat' | 'preventDefault'>
type KeyInput = { type: 'key'; keysym: number; down: boolean }

// RFB KeyEvent uses X11 keysyms (X11/keysymdef.h), not raw Unicode code points.
// Latin-1 characters keep their code point; other characters use the Unicode
// keysym prefix, e.g. U+4F60 (你) becomes 0x01004f60.
const LATIN1_MAX_CODE_POINT = 0xff
const UNICODE_KEYSYM_PREFIX = 0x01000000

// X11 modifier and special-key values, also defined by X11/keysymdef.h.
const SHIFT = 0xffe1
const CONTROL = 0xffe3
const ALT = 0xffe9
const META = 0xffeb
const modifiers = new Set([SHIFT, CONTROL, ALT, META])
const keysyms: Record<string, number> = {
  Backspace: 0xff08, Tab: 0xff09, Enter: 0xff0d, Escape: 0xff1b,
  Delete: 0xffff, Home: 0xff50, ArrowLeft: 0xff51, ArrowUp: 0xff52,
  ArrowRight: 0xff53, ArrowDown: 0xff54, PageUp: 0xff55, PageDown: 0xff56,
  End: 0xff57, Insert: 0xff63, Shift: SHIFT, Control: CONTROL, Alt: ALT, Meta: META,
}
const repeatingEdits = new Set(['Backspace', 'Delete', 'Home', 'End', 'ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'PageUp', 'PageDown'])

function keysymForKey(key: string): number | undefined {
  // Count code points rather than UTF-16 units, so an astral character such
  // as an emoji is treated as one character instead of an unknown key name.
  const characters = [...key]
  if (characters.length === 1) {
    const code = key.codePointAt(0)!
    return code <= LATIN1_MAX_CODE_POINT ? code : UNICODE_KEYSYM_PREFIX | code
  }
  if (/^F([1-9]|1[0-2])$/.test(key)) return 0xffbe + Number(key.slice(1)) - 1
  return keysyms[key]
}

export function createDisplayKeyboard(options: {
  mac: boolean
  active: () => boolean
  send: (event: KeyInput) => void
  copy: () => void
  pasteShortcut: (shift: boolean) => void
}) {
  const held = new Set<number>()
  // `KeyboardEvent.key` changes with modifier state (for example `A` becomes
  // `a` after Shift is released). Keep the keysym selected on keydown by the
  // physical key so keyup always releases the same remote key.
  const heldCodes = new Map<string, number>()
  const physical = new Set<number>()
  let needsReset = true

  function setKey(keysym: number, down: boolean) {
    if (held.has(keysym) === down) return
    if (down) held.add(keysym)
    else held.delete(keysym)
    options.send({ type: 'key', keysym, down })
  }

  function tap(keysym: number) {
    setKey(keysym, false)
    setKey(keysym, true)
    setKey(keysym, false)
  }

  function syncModifiers(event: KeyEvent) {
    if (needsReset) {
      // Recover modifiers/edit keys left down by an old pane or a missed
      // keyup before this keyboard controller acquired the connection.
      for (const keysym of [...modifiers, keysyms.Backspace!, keysyms.Delete!]) options.send({ type: 'key', keysym, down: false })
      needsReset = false
    }
    for (const [key, down] of [[SHIFT, event.shiftKey], [CONTROL, event.ctrlKey], [ALT, event.altKey], [META, !options.mac && event.metaKey]] as const) {
      if (down) physical.add(key)
      else physical.delete(key)
      // Defer Option until its accompanying key is known. Word editing uses
      // Control on Linux; pulsing Alt first can focus the application's menu.
      if (key === ALT && options.mac && event.key === 'Alt' && down) continue
      setKey(key, key === ALT && wordEdit(event) ? false : down)
    }
  }

  function wordEdit(event: KeyEvent) {
    return options.mac && event.altKey && !event.metaKey && !event.ctrlKey
      && ['Backspace', 'Delete'].includes(event.key)
  }

  function releaseCharacters() {
    for (const key of [...held]) if (!modifiers.has(key)) setKey(key, false)
    for (const [code, keysym] of heldCodes) {
      if (!modifiers.has(keysym)) heldCodes.delete(code)
    }
  }

  function command(event: KeyEvent, keysym: number) {
    const key = event.key.toLowerCase()
    if (key === 'c' || key === 'x') {
      if (event.repeat) return
      options.copy()
    }
    // Command is a local editing modifier, never Linux Super. Complete each
    // chord here so it cannot get stuck if the browser omits a character keyup.
    if (event.key === 'Backspace' || event.key === 'Delete') {
      setKey(SHIFT, true)
      tap(event.key === 'Backspace' ? keysyms.Home! : keysyms.End!)
      setKey(SHIFT, false)
      tap(keysym)
      setKey(SHIFT, event.shiftKey)
      return
    }
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      tap(event.key === 'ArrowLeft' ? keysyms.Home! : keysyms.End!)
      return
    }
    setKey(CONTROL, true)
    tap(event.key === 'ArrowUp' ? keysyms.Home! : event.key === 'ArrowDown' ? keysyms.End! : keysym)
    setKey(CONTROL, false)
  }

  function keydown(event: KeyEvent) {
    // Caps Lock may change case or switch the local input source. Return
    // before preventDefault() so the browser can handle it locally. Forward
    // the resulting characters, without toggling the remote Caps Lock state.
    if (!options.active() || event.key === 'CapsLock') return
    syncModifiers(event)
    const key = event.key.toLowerCase()
    const pasteModifier = options.mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
    if (pasteModifier && !event.altKey && key === 'v') {
      options.pasteShortcut(event.shiftKey)
      return // Preserve the native paste event and its clipboardData.
    }
    event.preventDefault()
    const keysym = keysymForKey(event.key)
    if (keysym === undefined || modifiers.has(keysym)) {
      if (options.mac && event.key === 'Meta') releaseCharacters()
      return
    }
    if (options.mac && event.metaKey && !event.ctrlKey && !event.altKey) {
      command(event, keysym)
      return
    }
    if (wordEdit(event)) {
      setKey(CONTROL, true)
      tap(keysym)
      setKey(CONTROL, false)
      return
    }
    if (repeatingEdits.has(event.key)) {
      // Use the local repeat cadence for editing, without leaving a key down
      // for the X server to repeat a second time or after the pane loses focus.
      tap(keysym)
      return
    }
    if (event.repeat) return
    if (event.ctrlKey && !event.altKey && ['c', 'x'].includes(key) && (!options.mac || event.shiftKey)) options.copy()
    setKey(keysym, true)
    if (event.code) heldCodes.set(event.code, keysym)
  }

  function keyup(event: KeyEvent) {
    // Match keydown's local-only Caps Lock handling; no remote key was pressed.
    if (!options.active() || event.key === 'CapsLock') return
    syncModifiers(event)
    if (options.mac && event.key === 'Meta') releaseCharacters()
    const keysym = event.code ? heldCodes.get(event.code) ?? keysymForKey(event.key) : keysymForKey(event.key)
    if (event.code) heldCodes.delete(event.code)
    if (keysym !== undefined) setKey(keysym, false)
  }

  function restoreModifiers() {
    if (!options.active()) return
    // The backend's atomic paste releases modifiers before Ctrl+V. Restore
    // only the keys that are still physically held after the native paste.
    for (const keysym of physical) options.send({ type: 'key', keysym, down: true })
  }

  function reset() {
    held.clear()
    heldCodes.clear()
    physical.clear()
    needsReset = true
  }

  function release() {
    for (const key of [...held].reverse()) setKey(key, false)
    heldCodes.clear()
    physical.clear()
    needsReset = true
  }

  function text(value: string) {
    if (!options.active()) return
    // Send only the text committed by the local input method, through RFB
    // key presses. The remote application does not need its own Chinese IME.
    // Release held keys first so Ctrl/Alt/Shift cannot turn it into a shortcut
    // or change its case; tap each character so none remains held remotely.
    release()
    for (const character of value) {
      const code = character.codePointAt(0)!
      // Newline and tab need X11 special keys. Skip the other ASCII controls
      // (below SPACE, 0x20, and DELETE, 0x7f) rather than typing them as text.
      const keysym = character === '\n' ? keysyms.Enter : character === '\t' ? keysyms.Tab
        : code >= 0x20 && code !== 0x7f ? keysymForKey(character) : undefined
      if (keysym !== undefined) tap(keysym)
    }
  }

  return { keydown, keyup, restoreModifiers, release, reset, text }
}
