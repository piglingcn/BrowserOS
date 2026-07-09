import { describe, expect, it } from 'bun:test'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'

const repoRoot = resolve(import.meta.dir, '../..')
const prepareClawServerRelease = join(
  repoRoot,
  'scripts/release/prepare-claw-server-release.sh',
)
const packagePath = 'packages/browseros-agent/apps/claw-server/package.json'
const lockPath = 'packages/browseros-agent/bun.lock'

async function run(
  cwd: string,
  args: string[],
): Promise<{ code: number; stdout: string; stderr: string }> {
  const proc = Bun.spawn(args, {
    cwd,
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const [stdout, stderr, code] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ])
  return { code, stdout, stderr }
}

async function mustRun(cwd: string, args: string[]): Promise<string> {
  const result = await run(cwd, args)
  expect(result.code, result.stderr || result.stdout).toBe(0)
  return result.stdout
}

function writePackage(dir: string, version: string): void {
  const path = join(dir, packagePath)
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(
    path,
    `${JSON.stringify({ name: '@browseros/claw-server', version }, null, 2)}\n`,
  )
}

function writeLock(dir: string, version: string): void {
  const path = join(dir, lockPath)
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(
    path,
    [
      '{',
      '  "workspaces": {',
      '    "apps/claw-server": {',
      '      "name": "@browseros/claw-server",',
      `      "version": "${version}",`,
      '    },',
      '  },',
      '}',
      '',
    ].join('\n'),
  )
}

async function commitPackage(dir: string, version: string): Promise<void> {
  writePackage(dir, version)
  await mustRun(dir, ['git', 'add', packagePath])
  await mustRun(dir, ['git', 'commit', '-m', `version ${version}`])
}

async function tag(dir: string, name: string): Promise<void> {
  await mustRun(dir, ['git', 'tag', '-a', name, '-m', name])
}

async function revParse(dir: string, ref: string): Promise<string> {
  return (await mustRun(dir, ['git', 'rev-parse', ref])).trim()
}

async function initFixture(version: string): Promise<{
  dir: string
  bareDir: string
}> {
  const dir = mkdtempSync(join(tmpdir(), 'claw-server-release-'))
  const bareDir = mkdtempSync(join(tmpdir(), 'claw-server-release-origin-'))
  await mustRun(dir, ['git', 'init', '--initial-branch=main'])
  await mustRun(dir, ['git', 'config', 'user.name', 'BrowserOS Test'])
  await mustRun(dir, ['git', 'config', 'user.email', 'test@browseros.com'])
  writePackage(dir, version)
  writeLock(dir, version)
  await mustRun(dir, ['git', 'add', '.'])
  await mustRun(dir, ['git', 'commit', '-m', `version ${version}`])
  await mustRun(bareDir, ['git', 'init', '--bare', '--initial-branch=main'])
  await mustRun(dir, ['git', 'remote', 'add', 'origin', bareDir])
  await mustRun(dir, ['git', 'push', '-u', 'origin', 'main'])
  return { dir, bareDir }
}

function parseOutput(stdout: string): Record<string, string> {
  return Object.fromEntries(
    stdout
      .trim()
      .split('\n')
      .filter(Boolean)
      .filter((line) => !line.startsWith('::'))
      .map((line) => line.split(/=(.*)/s).slice(0, 2)),
  )
}

function outputText(result: { stdout: string; stderr: string }): string {
  return `${result.stdout}\n${result.stderr}`
}

async function prepare(
  dir: string,
  options: {
    eventName: 'push' | 'workflow_dispatch' | 'workflow_call'
    refName?: string
    requestedVersion?: string
  },
) {
  return run(dir, [
    prepareClawServerRelease,
    '--event-name',
    options.eventName,
    '--default-branch',
    'main',
    '--ref-name',
    options.refName ?? 'main',
    '--requested-version',
    options.requestedVersion ?? '',
  ])
}

describe('prepare-claw-server-release', () => {
  it('creates a manual tag from the requested version', async () => {
    const { dir, bareDir } = await initFixture('0.0.2')
    try {
      const mainBefore = await revParse(bareDir, 'refs/heads/main')

      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.3',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(parseOutput(result.stdout)).toMatchObject({
        version: '0.0.3',
        tag: 'claw-server/v0.0.3',
        release_sha: mainBefore,
        previous_tag: '',
      })
      expect(await revParse(bareDir, 'claw-server/v0.0.3^{commit}')).toBe(
        mainBefore,
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('derives a workflow_call release version from package.json', async () => {
    const { dir, bareDir } = await initFixture('0.0.3')
    try {
      const releaseSha = await revParse(dir, 'HEAD')

      const result = await prepare(dir, {
        eventName: 'workflow_call',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(parseOutput(result.stdout)).toMatchObject({
        version: '0.0.3',
        tag: 'claw-server/v0.0.3',
        release_sha: releaseSha,
      })
      expect(await revParse(bareDir, 'claw-server/v0.0.3^{commit}')).toBe(
        releaseSha,
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('resolves pushed claw tags and previous tags', async () => {
    const { dir, bareDir } = await initFixture('0.0.2')
    try {
      await tag(dir, 'claw-server/v0.0.2')
      await commitPackage(dir, '0.0.3')
      await tag(dir, 'claw-server/v0.0.3')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'claw-server/v0.0.3',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(parseOutput(result.stdout)).toMatchObject({
        version: '0.0.3',
        tag: 'claw-server/v0.0.3',
        previous_tag: 'claw-server/v0.0.2',
      })
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects non-incrementing manual versions against legacy claw tags', async () => {
    const { dir, bareDir } = await initFixture('0.0.2')
    try {
      await tag(dir, 'claw-server-v0.0.3')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.2',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'Release version 0.0.2 must be greater than latest existing claw server version 0.0.3 (claw-server-v0.0.3)',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })
})
