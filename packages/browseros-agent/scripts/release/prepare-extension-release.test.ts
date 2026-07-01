import { describe, expect, it } from 'bun:test'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'

const repoRoot = resolve(import.meta.dir, '../..')
const prepareExtensionRelease = join(
  repoRoot,
  'scripts/release/prepare-extension-release.sh',
)
const packagePath = 'packages/browseros-agent/apps/app/package.json'
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
    `${JSON.stringify({ name: '@browseros/app', version }, null, 2)}\n`,
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
      '    "apps/app": {',
      '      "name": "@browseros/app",',
      `      "version": "${version}",`,
      '    },',
      '  },',
      '}',
      '',
    ].join('\n'),
  )
}

async function commitReleaseFiles(dir: string, version: string): Promise<void> {
  writePackage(dir, version)
  writeLock(dir, version)
  await mustRun(dir, ['git', 'add', packagePath, lockPath])
  await mustRun(dir, ['git', 'commit', '-m', `version ${version}`])
}

async function commitPackage(dir: string, version: string): Promise<void> {
  writePackage(dir, version)
  await mustRun(dir, ['git', 'add', packagePath])
  await mustRun(dir, ['git', 'commit', '-m', `package version ${version}`])
}

async function tag(dir: string, name: string): Promise<void> {
  await mustRun(dir, ['git', 'tag', '-a', name, '-m', name])
}

async function initFixture(
  version: string,
  lockVersion = version,
): Promise<{
  dir: string
  bareDir: string
}> {
  const dir = mkdtempSync(join(tmpdir(), 'extension-release-'))
  const bareDir = mkdtempSync(join(tmpdir(), 'extension-release-origin-'))
  await mustRun(dir, ['git', 'init', '--initial-branch=main'])
  await mustRun(dir, ['git', 'config', 'user.name', 'BrowserOS Test'])
  await mustRun(dir, ['git', 'config', 'user.email', 'test@browseros.com'])
  writePackage(dir, version)
  writeLock(dir, lockVersion)
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
    eventName: 'push' | 'workflow_dispatch'
    refName?: string
    requestedVersion?: string
  },
) {
  return run(dir, [
    prepareExtensionRelease,
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

describe('prepare-extension-release', () => {
  it('commits a manual version bump and pushes the branch and tag', async () => {
    const { dir, bareDir } = await initFixture('0.0.100')
    try {
      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.101',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      const output = parseOutput(result.stdout)
      expect(output).toMatchObject({
        version: '0.0.101',
        tag: 'agent-extension/v0.0.101',
        previous_tag: '',
      })
      expect(
        await mustRun(dir, ['git', 'show', `origin/main:${packagePath}`]),
      ).toContain('"version": "0.0.101"')
      expect(
        await mustRun(dir, ['git', 'show', `origin/main:${lockPath}`]),
      ).toContain('"version": "0.0.101"')
      expect(
        (
          await mustRun(dir, [
            'git',
            'cat-file',
            '-t',
            'refs/tags/agent-extension/v0.0.101',
          ])
        ).trim(),
      ).toBe('tag')
      expect(
        (
          await mustRun(dir, [
            'git',
            'rev-list',
            '-n',
            '1',
            'agent-extension/v0.0.101',
          ])
        ).trim(),
      ).toBe((await mustRun(dir, ['git', 'rev-parse', 'origin/main'])).trim())
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('creates a manual tag for the current package version without preconfigured identity', async () => {
    const { dir, bareDir } = await initFixture('0.0.101')
    try {
      const releaseSha = (
        await mustRun(dir, ['git', 'rev-parse', 'HEAD'])
      ).trim()
      await mustRun(dir, ['git', 'config', '--unset', 'user.name'])
      await mustRun(dir, ['git', 'config', '--unset', 'user.email'])

      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.101',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(parseOutput(result.stdout)).toMatchObject({
        version: '0.0.101',
        tag: 'agent-extension/v0.0.101',
        release_sha: releaseSha,
      })
      expect(
        (
          await mustRun(dir, [
            'git',
            'rev-list',
            '-n',
            '1',
            'agent-extension/v0.0.101',
          ])
        ).trim(),
      ).toBe(releaseSha)
      expect(
        (await mustRun(dir, ['git', 'rev-parse', 'origin/main'])).trim(),
      ).toBe(releaseSha)
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('repairs a stale lockfile entry before tagging an already-bumped package', async () => {
    const { dir, bareDir } = await initFixture('0.0.101', '0.0.100')
    try {
      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.101',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(
        await mustRun(dir, ['git', 'show', `origin/main:${packagePath}`]),
      ).toContain('"version": "0.0.101"')
      expect(
        await mustRun(dir, ['git', 'show', `origin/main:${lockPath}`]),
      ).toContain('"version": "0.0.101"')
      expect(
        (
          await mustRun(dir, [
            'git',
            'rev-list',
            '-n',
            '1',
            'agent-extension/v0.0.101',
          ])
        ).trim(),
      ).toBe((await mustRun(dir, ['git', 'rev-parse', 'origin/main'])).trim())
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('resolves a pushed extension tag and previous release tag', async () => {
    const { dir, bareDir } = await initFixture('0.0.100')
    try {
      await tag(dir, 'agent-extension-v0.0.100')
      await commitReleaseFiles(dir, '0.0.101')
      await tag(dir, 'agent-extension/v0.0.101')
      await commitReleaseFiles(dir, '0.0.102')
      await tag(dir, 'agent-extension/v0.0.102')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.102',
      })

      expect(result.code, result.stderr || result.stdout).toBe(0)
      expect(parseOutput(result.stdout)).toMatchObject({
        version: '0.0.102',
        tag: 'agent-extension/v0.0.102',
        previous_tag: 'agent-extension/v0.0.101',
      })
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects a pushed tag whose commit is not on the default branch', async () => {
    const { dir, bareDir } = await initFixture('0.0.100')
    try {
      await mustRun(dir, ['git', 'checkout', '-b', 'release-side'])
      await commitReleaseFiles(dir, '0.0.101')
      await tag(dir, 'agent-extension/v0.0.101')
      await mustRun(dir, ['git', 'push', 'origin', 'agent-extension/v0.0.101'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain('is not reachable from origin/main')
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects a release version that already exists as a legacy tag', async () => {
    const { dir, bareDir } = await initFixture('0.0.101')
    try {
      await tag(dir, 'agent-extension-v0.0.101')
      await tag(dir, 'agent-extension/v0.0.101')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'Release version 0.0.101 already exists as tag agent-extension-v0.0.101',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects a non-incrementing release version', async () => {
    const { dir, bareDir } = await initFixture('0.0.102')
    try {
      await tag(dir, 'agent-extension-v0.0.102')
      await commitReleaseFiles(dir, '0.0.101')
      await tag(dir, 'agent-extension/v0.0.101')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'Release version 0.0.101 must be greater than latest existing extension version 0.0.102',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects manual downgrades below the current package version', async () => {
    const { dir, bareDir } = await initFixture('0.0.102')
    try {
      const result = await prepare(dir, {
        eventName: 'workflow_dispatch',
        requestedVersion: '0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'Requested extension version 0.0.101 is lower than packages/browseros-agent/apps/app/package.json (0.0.102)',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects a pushed tag whose package version does not match', async () => {
    const { dir, bareDir } = await initFixture('0.0.100')
    try {
      await tag(dir, 'agent-extension/v0.0.101')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'packages/browseros-agent/apps/app/package.json at agent-extension/v0.0.101 is 0.0.100, expected 0.0.101',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })

  it('rejects a pushed tag whose lockfile version does not match', async () => {
    const { dir, bareDir } = await initFixture('0.0.100')
    try {
      await commitPackage(dir, '0.0.101')
      await tag(dir, 'agent-extension/v0.0.101')
      await mustRun(dir, ['git', 'push', 'origin', 'main', '--tags'])

      const result = await prepare(dir, {
        eventName: 'push',
        refName: 'agent-extension/v0.0.101',
      })

      expect(result.code).toBe(1)
      expect(outputText(result)).toContain(
        'packages/browseros-agent/bun.lock at agent-extension/v0.0.101 has apps/app version 0.0.100, expected 0.0.101',
      )
    } finally {
      rmSync(dir, { recursive: true, force: true })
      rmSync(bareDir, { recursive: true, force: true })
    }
  })
})
