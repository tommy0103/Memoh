import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { appendFileSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { gunzipSync } from 'node:zlib'
import notarizeDmg from './notarize-dmg.mjs'

const env = {
  CSC_LINK: 'test-certificate', CSC_KEY_PASSWORD: 'test-password',
  APPLE_API_KEY: '/test/key.p8', APPLE_API_KEY_ID: 'test-key', APPLE_API_ISSUER: 'test-issuer',
}

function artifact(t) {
  const dir = mkdtempSync(join(tmpdir(), 'memoh-dmg-test-'))
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  const file = join(dir, 'test.dmg')
  writeFileSync(file, 'signed DMG fixture')
  return { file, updateInfo: { size: 18, sha512: 'stale' } }
}

test('only notarizes this build event DMG when signing credentials are configured', async () => {
  const run = () => assert.fail('unsigned builds and other artifacts must not invoke Apple tools')
  for (const file of ['app.zip', 'app.dmg.blockmap', undefined]) {
    await notarizeDmg({ file }, { env, run })
  }
  await notarizeDmg({ file: 'app.dmg' }, { env: {}, run })
})

test('staples and verifies before rebuilding actual blockmap and manifest metadata', async (t) => {
  const event = artifact(t)
  const calls = []
  await notarizeDmg(event, { env, run(command, args) {
    calls.push([command, ...args])
    if (args[0] === 'notarytool') {
      assert.ok(args.includes('--wait'))
      return JSON.stringify({ status: 'Accepted', id: 'submission' })
    }
    if (args[0] === 'stapler' && args[1] === 'staple') appendFileSync(event.file, ' notarization ticket')
    assert.equal(event.updateInfo.sha512, 'stale')
  } })
  assert.deepEqual(calls.map(call => call.slice(0, 3)), [
    ['codesign', '--verify', '--strict'], ['xcrun', 'notarytool', 'submit'],
    ['xcrun', 'stapler', 'staple'], ['xcrun', 'stapler', 'validate'],
    ['codesign', '--verify', '--strict'], ['spctl', '--assess', '--type'],
    ['hdiutil', 'verify', event.file],
  ])
  const bytes = readFileSync(event.file)
  assert.equal(event.updateInfo.sha512, createHash('sha512').update(bytes).digest('base64'))
  assert.equal(event.updateInfo.size, bytes.length)
  const blockmap = JSON.parse(gunzipSync(readFileSync(`${event.file}.blockmap`)))
  assert.equal(blockmap.files.flatMap(file => file.sizes).reduce((a, b) => a + b, 0), bytes.length)
})

for (const status of ['Invalid', 'In Progress', undefined]) {
  test(`does not continue or refresh metadata when notarization status is ${status}`, async (t) => {
    const event = artifact(t)
    await assert.rejects(notarizeDmg(event, { env, run(command, args) {
      if (args[0] === 'notarytool') return JSON.stringify({ status, id: 'rejected' })
      assert.equal(command, 'codesign')
    } }), /DMG notarization failed/)
    assert.equal(event.updateInfo.sha512, 'stale')
    assert.equal(existsSync(`${event.file}.blockmap`), false)
  })
}

for (const failure of ['notarytool', 'staple', 'validate', 'spctl', 'hdiutil']) {
  test(`aborts build when ${failure} fails`, async (t) => {
    const event = artifact(t)
    await assert.rejects(notarizeDmg(event, { env, run(command, args) {
      if (command === failure || args.includes(failure)) throw new Error(`${failure} failed`)
      if (args[0] === 'notarytool') return JSON.stringify({ status: 'Accepted', id: 'submission' })
    } }), new RegExp(`${failure} failed`))
    assert.equal(event.updateInfo.sha512, 'stale')
    assert.equal(existsSync(`${event.file}.blockmap`), false)
  })
}
