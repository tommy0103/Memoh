export interface RuntimeCommandCredential {
  key?: string
  team_id?: string
}

export type RuntimeConnectMode = 'connect' | 'replace'

export function buildRuntimeConnectCommand(
  serverUrl: string,
  credential: RuntimeCommandCredential | null | undefined,
  mode: RuntimeConnectMode = 'connect',
): string {
  const key = credential?.key?.trim()
  if (!key) return ''

  const args = [
    'memoh-runtime',
    'enroll',
    '--server',
    serverUrl,
    '--key',
    key,
  ]
  const teamId = credential?.team_id?.trim()
  if (teamId) {
    args.push('--team-id', teamId)
  }
  if (isInsecureLocalhost(serverUrl)) {
    args.push('--insecure-localhost')
  }
  if (mode === 'replace') {
    args.push('--replace')
  }
  // Stop on failure so a rejected enrollment cannot start a different saved connection.
  return [
    'npm install -g @memohai/runtime@latest',
    args.join(' '),
    'memoh-runtime service install',
    'memoh-runtime service start',
  ].join(' && ')
}

function isInsecureLocalhost(serverUrl: string): boolean {
  const url = new URL(serverUrl)
  const hostname = url.hostname.replace(/^\[|\]$/g, '')
  return url.protocol === 'http:' && ['localhost', '127.0.0.1', '::1'].includes(hostname)
}
