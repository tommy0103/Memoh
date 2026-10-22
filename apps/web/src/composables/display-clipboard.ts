export const MAX_CLIPBOARD_BYTES = 48 * 1024
export const MAX_CLIPBOARD_MESSAGE_BYTES = 64 * 1024

export function encodeClipboardPaste(text: string, shift = false): string | null {
  return encodeClipboardText(text, 'clipboard', shift)
}

function encodeClipboardText(text: string, type: string, shift = false): string | null {
  if (text.includes('\0') || new TextEncoder().encode(text).length > MAX_CLIPBOARD_BYTES) return null
  const message = JSON.stringify({ type, text, ...(shift ? { shift: true } : {}) })
  return new TextEncoder().encode(message).length <= MAX_CLIPBOARD_MESSAGE_BYTES ? message : null
}

export function decodeClipboardMessage(data: unknown): Record<string, unknown> | null {
  if (typeof data !== 'string' || new TextEncoder().encode(data).length > MAX_CLIPBOARD_MESSAGE_BYTES) return null
  try {
    const value: unknown = JSON.parse(data)
    if (!value || typeof value !== 'object') return null
    const message = value as Record<string, unknown>
    if (message.type === 'clipboard' && typeof message.text === 'string'
      && !message.text.includes('\0')
      && new TextEncoder().encode(message.text).length <= MAX_CLIPBOARD_BYTES) return message
    if (message.type === 'clipboard_error' && typeof message.code === 'string') return message
  } catch { /* Ignore malformed messages without affecting the video connection. */ }
  return null
}
