import { describe, expect, it } from 'vitest'
import { macWindowChromeOptions } from './window-chrome'

describe('macWindowChromeOptions', () => {
  it('uses hidden title-bar chrome on macOS', () => {
    expect(macWindowChromeOptions('darwin', 'memoh-chat')).toMatchObject({
      tabbingIdentifier: 'memoh-chat',
      titleBarStyle: 'hidden',
      trafficLightPosition: { x: 14, y: 13 },
    })
  })

  it('leaves other platforms on their standard opaque window chrome', () => {
    expect(macWindowChromeOptions('win32', 'memoh-chat')).toEqual({})
    expect(macWindowChromeOptions('linux', 'memoh-chat')).toEqual({})
  })
})
