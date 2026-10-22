import type { BrowserWindowConstructorOptions } from 'electron'

export function macWindowChromeOptions(
  platform: NodeJS.Platform,
  tabbingIdentifier: string,
): Partial<BrowserWindowConstructorOptions> {
  if (platform !== 'darwin') return {}
  return {
    titleBarStyle: 'hidden',
    trafficLightPosition: { x: 14, y: 13 },
    tabbingIdentifier,
  }
}
