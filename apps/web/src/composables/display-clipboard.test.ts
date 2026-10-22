import { describe, expect, it } from 'vitest'
import { decodeClipboardMessage, encodeClipboardPaste, MAX_CLIPBOARD_BYTES, MAX_CLIPBOARD_MESSAGE_BYTES } from './display-clipboard'

describe('display clipboard protocol', () => {
  it('limits UTF-8 and JSON wire bytes independently', () => {
    expect(encodeClipboardPaste('中'.repeat(MAX_CLIPBOARD_BYTES / 3))).not.toBeNull()
    expect(encodeClipboardPaste('中'.repeat(MAX_CLIPBOARD_BYTES / 3 + 1))).toBeNull()
    expect(encodeClipboardPaste('\u0001'.repeat(MAX_CLIPBOARD_BYTES))).toBeNull()
    expect(encodeClipboardPaste('contains\0NUL')).toBeNull()
    expect(decodeClipboardMessage(' '.repeat(MAX_CLIPBOARD_MESSAGE_BYTES + 1))).toBeNull()
    expect(decodeClipboardMessage(JSON.stringify({ type: 'clipboard', text: 'x'.repeat(MAX_CLIPBOARD_BYTES + 1) }))).toBeNull()
  })

  it('ignores malformed and unrelated messages while retaining stable error codes', () => {
    for (const data of [null, {}, '{', 'null', '[]', '{"type":"key"}', '{"type":"clipboard","text":1}']) {
      expect(decodeClipboardMessage(data)).toBeNull()
    }
    expect(decodeClipboardMessage('{"type":"clipboard_error","code":"workspace.display_clipboard_failed"}')?.code).toBe('workspace.display_clipboard_failed')
  })
})
