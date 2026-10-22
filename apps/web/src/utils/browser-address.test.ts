import { describe, expect, it } from 'vitest'
import { parseBrowserAddress } from './browser-address'

describe('parseBrowserAddress', () => {
  it('parses localhost addresses', () => {
    expect(parseBrowserAddress('localhost:3000/foo?x=1')).toEqual({
      port: 3000,
      path: '/foo?x=1',
      display: 'localhost:3000/foo?x=1',
    })
  })

  it('parses loopback addresses', () => {
    expect(parseBrowserAddress('127.0.0.1:8000')).toEqual({
      port: 8000,
      path: '/',
      display: 'localhost:8000/',
    })
  })

  it('parses port shorthand', () => {
    expect(parseBrowserAddress(':5173')).toEqual({
      port: 5173,
      path: '/',
      display: 'localhost:5173/',
    })
  })

  it('rejects non-loopback hosts', () => {
    expect(() => parseBrowserAddress('example.com:3000')).toThrow(/localhost/)
  })

  it('rejects invalid ports', () => {
    expect(() => parseBrowserAddress('localhost:99999')).toThrow(/port/)
  })
})
