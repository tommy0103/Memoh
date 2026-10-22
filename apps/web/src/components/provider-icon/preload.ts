import { shallowRef, type ShallowRef } from 'vue'

// Share the fetched bytes, not just a detached Image that merely warms the
// browser's HTTP cache. A remounted icon can reuse this source without another
// remote resource request. Data URLs need no revocation while a consumer uses
// them; the bounded map limits how many unused sources we retain.
const sources = new Map<string, ShallowRef<string | null>>()
const maxEntries = 128
const maxBytes = 512 * 1024
const retryAfter = new Map<string, number>()
let remoteLoader: ((url: string) => Promise<string>) | undefined

// Desktop supplies a cookie-free native image transport before mounting. Web
// retains browser fetch and the ordinary <img> fallback for non-CORS hosts.
export function configureProviderIconLoader(loader: (url: string) => Promise<string>): void {
  remoteLoader = loader
}

export function providerIconSource(url: string): ShallowRef<string | null> {
  const cached = sources.get(url)
  if (cached && (retryAfter.get(url) ?? Infinity) > Date.now()) {
    sources.delete(url)
    sources.set(url, cached)
    return cached
  }
  retryAfter.delete(url)
  // Empty means loading; null means the native transport failed or rejected the URL.
  const source = shallowRef<string | null>('')
  sources.set(url, source)
  if (sources.size > maxEntries) {
    const oldest = sources.keys().next().value!
    sources.delete(oldest)
    retryAfter.delete(oldest)
  }
  void load(url, source)
  return source
}

async function load(url: string, source: ShallowRef<string | null>): Promise<void> {
  try {
    const dataUrl = remoteLoader ? await remoteLoader(url) : await fetchDataUrl(url)
    const image = new Image()
    image.src = dataUrl
    await image.decode()
    source.value = dataUrl
  } catch {
    // Never retry a native rejection through Chromium: that would bypass the
    // public-destination policy and could send authenticated image requests.
    source.value = remoteLoader ? null : url
    // Keep failed work shared briefly; preloading and menu remounts must not
    // immediately hammer the same unavailable host. A later mount can recover.
    if (sources.get(url) === source) retryAfter.set(url, Date.now() + 30_000)
  }
}

async function fetchDataUrl(url: string): Promise<string> {
  const response = await fetch(url)
  if (!response.ok) throw new Error('Icon request failed')
  const blob = await response.blob()
  if (!blob.type.startsWith('image/') || blob.size > maxBytes) throw new Error('Icon cannot be cached')
  const bytes = new Uint8Array(await blob.arrayBuffer())
  const encoded = btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''))
  return `data:${blob.type};base64,${encoded}`
}

export function preloadProviderIcons(icons: Iterable<string | undefined>): void {
  if (typeof Image === 'undefined') return
  for (const icon of icons) {
    if (icon && /^https?:\/\//.test(icon)) providerIconSource(icon)
  }
}
