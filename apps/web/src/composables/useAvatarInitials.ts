import { computed } from 'vue'

const graphemes = typeof Intl.Segmenter === 'function'
  ? new Intl.Segmenter(undefined, { granularity: 'grapheme' })
  : undefined

// Keep surrogate pairs, modifiers and joined emoji together.
export function firstAvatarCharacter(text: string): string {
  // Older browsers retain a complete code point, without grapheme grouping.
  if (!graphemes) return text[Symbol.iterator]().next().value ?? ''
  return graphemes.segment(text)[Symbol.iterator]().next().value?.segment ?? ''
}

// Single source for the "first letters as an avatar fallback" rule. Use the pure
// avatarInitials() in lists or one-off calls; useAvatarInitials() wraps it for a
// reactive label and returns a computed.
export function avatarInitials(label: string | null | undefined, fallback = '') {
  const text = label?.trim() ?? ''
  if (!text) {
    return fallback
  }
  return text.slice(0, 2).toUpperCase() || fallback
}

export function useAvatarInitials(getLabel: () => string | null | undefined, fallback = '') {
  return computed(() => avatarInitials(getLabel(), fallback))
}
