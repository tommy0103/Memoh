import { describe, expect, it, vi } from 'vitest'
import { firstAvatarCharacter } from './useAvatarInitials'

describe('firstAvatarCharacter', () => {
  it.each(['😁', '👍🏽', '👨‍👩‍👧‍👦', '🇨🇳', 'é', '中', 'A'])(
    'keeps the complete first character in %s', (character) => {
      expect(firstAvatarCharacter(`${character}bot`)).toBe(character)
    },
  )
  it('handles an empty name', () => {
    expect(firstAvatarCharacter('')).toBe('')
  })
})

describe('without Intl.Segmenter', () => {
  it('loads the module and falls back to complete code points', async () => {
    vi.stubGlobal('Intl', Object.create(Intl, { Segmenter: { value: undefined } }))
    vi.resetModules()
    try {
      const { firstAvatarCharacter: first } = await import('./useAvatarInitials')
      expect(first('😁bot')).toBe('😁')
      expect(first('👍🏽')).toBe('👍')
      expect(first('👨‍👩‍👧‍👦')).toBe('👨')
      expect(first('中文')).toBe('中')
      expect(first('Alice')).toBe('A')
      expect(first('')).toBe('')
    } finally {
      vi.unstubAllGlobals()
      vi.resetModules()
    }
  })
})
