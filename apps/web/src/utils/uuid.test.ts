import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomUUID } from './uuid'

afterEach(() => vi.unstubAllGlobals())

describe('randomUUID', () => {
  it('uses the native method with its Crypto receiver', () => {
    const expected = '12345678-1234-4123-8123-123456789abc'
    const source = {
      randomUUID: vi.fn(function (this: unknown) {
        expect(this).toBe(source)
        return expected
      }),
      getRandomValues: vi.fn(),
    }
    vi.stubGlobal('crypto', source)
    expect(randomUUID()).toBe(expected)
    expect(source.randomUUID).toHaveBeenCalledOnce()
    expect(source.getRandomValues).not.toHaveBeenCalled()
  })

  it.each([
    [0, '00000000-0000-4000-8000-000000000000'],
    [255, 'ffffffff-ffff-4fff-bfff-ffffffffffff'],
  ])('sets RFC 4122 version and variant bits for entropy byte %i', (byte, expected) => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => {
      expect(bytes).toBeInstanceOf(Uint8Array)
      expect(bytes.byteLength).toBe(16)
      return bytes.fill(byte)
    })
    vi.stubGlobal('crypto', { getRandomValues })
    expect(randomUUID()).toBe(expected)
    expect(getRandomValues).toHaveBeenCalledOnce()
  })

  it('gets fresh secure entropy on each HTTP fallback call', () => {
    let value = 0
    const getRandomValues = vi.fn((bytes: Uint8Array) => bytes.fill(++value))
    vi.stubGlobal('crypto', { getRandomValues })
    const first = randomUUID()
    const second = randomUUID()
    expect(first).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(first).not.toBe(second)
    expect(getRandomValues).toHaveBeenCalledTimes(2)
  })

  it('fails explicitly when secure randomness is unavailable', () => {
    vi.stubGlobal('crypto', undefined)
    expect(randomUUID).toThrow('Secure random number generation is unavailable')
  })
})
