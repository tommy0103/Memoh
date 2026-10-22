import { describe, expect, it } from 'vitest'

import { buildRuntimeConnectCommand } from './command'

const key = `mrk_${'a'.repeat(64)}`
const teamId = '11111111-1111-4111-8111-111111111111'

describe('buildRuntimeConnectCommand', () => {
  it('includes the credential team ID required by hosted gateways', () => {
    expect(buildRuntimeConnectCommand('https://memoh.example/api', {
      key,
      team_id: teamId,
    })).toBe(
      `npm install -g @memohai/runtime@latest && memoh-runtime enroll --server https://memoh.example/api --key ${key} --team-id ${teamId} && memoh-runtime service install && memoh-runtime service start`,
    )
  })

  it('keeps credentials from older self-hosted servers usable', () => {
    expect(buildRuntimeConnectCommand('https://memoh.example/api', { key }))
      .toBe(`npm install -g @memohai/runtime@latest && memoh-runtime enroll --server https://memoh.example/api --key ${key} && memoh-runtime service install && memoh-runtime service start`)
  })

  it('enables plaintext WebSockets only for loopback development servers', () => {
    expect(buildRuntimeConnectCommand('http://127.0.0.1:18080', {
      key,
      team_id: teamId,
    })).toBe(
      `npm install -g @memohai/runtime@latest && memoh-runtime enroll --server http://127.0.0.1:18080 --key ${key} --team-id ${teamId} --insecure-localhost && memoh-runtime service install && memoh-runtime service start`,
    )
  })

  it('replaces saved enrollment only when rebinding is explicitly selected', () => {
    const credential = { key, team_id: teamId }
    const firstConnection = buildRuntimeConnectCommand('http://localhost:18083/api', credential)
    const rebind = buildRuntimeConnectCommand('http://localhost:18083/api', credential, 'replace')
    expect(firstConnection).not.toContain('--replace')
    expect(rebind).toBe(firstConnection.replace(
      '--insecure-localhost &&', '--insecure-localhost --replace &&',
    ))
  })
})
