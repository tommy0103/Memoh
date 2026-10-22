import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { hasMacNotarizationEnv } from './build-env.mjs'

// Resolve through the installed packager so its blockmap format and hashing
// match the version that generated the rest of this build's update artifacts.
const builderRequire = createRequire(import.meta.resolve('electron-builder'))
const { buildBlockMap } = builderRequire('app-builder-lib/out/targets/blockmap/blockmap.js')

export default async function notarizeDmg(event, { env = process.env, run = execFileSync } = {}) {
  if (!event.file?.endsWith('.dmg') || !hasMacNotarizationEnv(env)) return

  const file = event.file
  const options = { env, stdio: 'inherit' }
  run('codesign', ['--verify', '--strict', '--verbose=2', file], options)
  console.log(`Notarizing DMG: ${file}`)
  const result = JSON.parse(run('xcrun', [
    'notarytool', 'submit', file,
    '--key', env.APPLE_API_KEY,
    '--key-id', env.APPLE_API_KEY_ID,
    '--issuer', env.APPLE_API_ISSUER,
    '--wait', '--timeout', '20m', '--output-format', 'json',
  ], { ...options, stdio: ['ignore', 'pipe', 'inherit'], encoding: 'utf8' }))
  if (result.status !== 'Accepted') {
    throw new Error(`DMG notarization failed: ${result.status} (submission ${result.id})`)
  }
  console.log(`DMG notarization accepted: ${result.id}`)
  run('xcrun', ['stapler', 'staple', file], options)
  run('xcrun', ['stapler', 'validate', file], options)
  run('codesign', ['--verify', '--strict', '--verbose=2', file], options)
  run('spctl', ['--assess', '--type', 'open', '--context', 'context:primary-signature', '--verbose=2', file], options)
  run('hdiutil', ['verify', file], options)

  // artifactBuildCompleted runs before artifactCreated consumes updateInfo.
  // Stapling changes DMG bytes after electron-builder's first blockmap pass:
  // replace both the sidecar and event metadata before manifests are generated.
  // build.mjs enforces --publish never; uploads happen only after build success.
  if (event.updateInfo != null) {
    event.updateInfo = await buildBlockMap(file, 'gzip', `${file}.blockmap`)
  }
}
