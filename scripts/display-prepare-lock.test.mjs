import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, readlink, writeFile, mkdir, rm, access } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'

const source = await readFile(new URL('./display-prepare.sh', import.meta.url), 'utf8')
const block = source.slice(source.indexOf('prepare_lock=/tmp/'), source.indexOf('progress 10 checking'))
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))

async function fixture(t) {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'memoh-lock-test-'))
  const children = []
  t.after(async () => {
    for (const child of children) {
      try { process.kill(-child.pid, 'SIGKILL') } catch { /* already exited */ }
    }
    await rm(dir, { recursive: true, force: true })
  })
  const lock = path.join(dir, 'lock')
  const guard = path.join(dir, 'guard')
  const script = path.join(dir, 'display-prepare.sh')
  await writeFile(script, '#!/bin/sh\nset -eu\nprogress() { :; }\ndisplay_ready() { return 1; }\nemit_complete() { :; }\n'
    + block.replace('prepare_lock=/tmp/memoh-display-prepare.lock', `prepare_lock='${lock}'`)
      .replace('prepare_guard=/tmp/memoh-display-prepare.guard', `prepare_guard='${guard}'`)
    + '\nprintf "start:%s\\n" "$$" >>"$EVENTS"\nsleep "${HOLD_SECONDS:-0}"\nprintf "end:%s\\n" "$$" >>"$EVENTS"\n')
  const events = path.join(dir, 'events')
  await writeFile(events, '')
  function run(hold = '0') {
    const child = spawn('/bin/sh', [script], {
      detached: true, env: { ...process.env, EVENTS: events, HOLD_SECONDS: hold }, stdio: ['ignore', 'ignore', 'pipe'],
    })
    children.push(child)
    let stderr = ''
    child.stderr.on('data', data => { stderr += data })
    const done = new Promise((resolve, reject) => {
      child.on('error', reject)
      child.on('exit', (code, signal) => resolve({ code, signal, stderr }))
    })
    return { child, done }
  }
  async function waitFor(predicate) {
    const deadline = Date.now() + 8000
    while (!await predicate()) {
      assert.ok(Date.now() < deadline, 'timed out waiting for lock test')
      await delay(20)
    }
  }
  return { lock, events, run, waitFor }
}

const linux = { skip: process.platform !== 'linux', timeout: 15000 }
for (const pid of [null, '99999999']) {
  test(`recovers a stale ${pid ? 'PID' : 'legacy empty'} lock`, linux, async t => {
    const f = await fixture(t)
    await mkdir(f.lock)
    if (pid) await writeFile(path.join(f.lock, 'pid'), pid)
    assert.equal((await f.run().done).code, 0)
    await assert.rejects(access(f.lock))
  })
}

test('concurrent recovery has one owner and releases before the next enters', linux, async t => {
  const f = await fixture(t)
  await mkdir(f.lock)
  const a = f.run('0.2')
  const b = f.run('0.2')
  assert.equal((await a.done).code, 0)
  assert.equal((await b.done).code, 0)
  const events = (await readFile(f.events, 'utf8')).trim().split('\n')
  assert.equal(events.length, 4)
  assert.equal(events[0].replace('start:', ''), events[1].replace('end:', ''))
  assert.equal(events[2].replace('start:', ''), events[3].replace('end:', ''))
})

test('recovers a killed new owner and leaves unknown lock contents intact', linux, async t => {
  const f = await fixture(t)
  const owner = f.run('30')
  await f.waitFor(async () => (await readFile(f.events, 'utf8')).includes('start:'))
  // Processes the owner starts from here on cannot inherit the guard.
  await assert.rejects(readlink(`/proc/${owner.child.pid}/fd/9`))
  process.kill(-owner.child.pid, 'SIGKILL')
  await owner.done
  assert.equal((await f.run().done).code, 0)
  await mkdir(f.lock)
  await writeFile(path.join(f.lock, 'unknown'), 'keep')
  const waiter = f.run()
  await delay(200)
  assert.equal(await readFile(path.join(f.lock, 'unknown'), 'utf8'), 'keep')
  process.kill(-waiter.child.pid, 'SIGKILL')
  await waiter.done
})
