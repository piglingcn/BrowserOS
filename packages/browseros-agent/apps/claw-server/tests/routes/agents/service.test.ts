/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { describe, expect, test } from 'bun:test'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { env } from '../../../src/env'
import * as agents from '../../../src/routes/agents/service'
import { writeAgentProfile } from '../../_helpers/agent-profile'
import { withTempBrowserClawDir } from '../../_helpers/temp-browserclaw-dir'

async function withProxyPort<T>(
  port: number,
  fn: () => Promise<T>,
): Promise<T> {
  const previous = env.proxyPort
  env.proxyPort = port
  try {
    return await fn()
  } finally {
    env.proxyPort = previous
  }
}

describe('agents service', () => {
  test('list returns the directory projection with derived fields', async () => {
    await withTempBrowserClawDir(async () => {
      await writeAgentProfile({
        name: 'Selective Agent',
        loginMode: 'selective',
        selectedSites: ['concur.com', 'stripe.com', 'ramp.com'],
        aclRuleIds: ['a', 'b', 'c'],
        approvals: { submit: 'Block', payment: 'Block', input: 'Auto' },
      })
      const rows = await agents.list()
      expect(rows).toHaveLength(1)
      const row = rows[0]
      expect(row.loginScopeLabel).toBe('Selective (3)')
      expect(row.loginCount).toBe(3)
      expect(row.aclRuleCount).toBe(3)
      expect(row.blockedActionCount).toBe(2)
      expect(row.alwaysAllowCount).toBe(0)
      expect(row.lastRunAt).toBe('Never run')
      expect(row.status).toBe('configured')
      expect(row.mcpUrl).toBe('http://127.0.0.1:9200/mcp')
    })
  })

  test('list derives visible MCP URLs from the trusted proxy MCP base URL', async () => {
    await withProxyPort(9512, async () => {
      await withTempBrowserClawDir(async () => {
        await writeAgentProfile({ name: 'Listed Proxy' })
        const rows = await agents.list()
        expect(rows[0]?.mcpUrl).toBe('http://127.0.0.1:9512/mcp')
      })
    })
  })

  test('list sorts by updatedAt descending', async () => {
    await withTempBrowserClawDir(async () => {
      const older = await writeAgentProfile({
        id: 'alpha',
        name: 'Alpha',
        slug: 'alpha',
        updatedAt: '2026-07-06T00:00:00.000Z',
      })
      const newer = await writeAgentProfile({
        id: 'beta',
        name: 'Beta',
        slug: 'beta',
        updatedAt: '2026-07-06T00:00:01.000Z',
      })
      const rows = await agents.list()
      expect(rows.map((row) => row.id)).toEqual([newer.id, older.id])
    })
  })

  test('list skips a corrupt agent file instead of rejecting the whole call', async () => {
    await withTempBrowserClawDir(async (dir) => {
      const ok = await writeAgentProfile({ id: 'healthy', name: 'Healthy' })
      await writeFile(
        join(dir, 'agents', 'broken.json'),
        '{ this is not valid json',
        'utf8',
      )
      const rows = await agents.list()
      expect(rows.map((row) => row.id)).toEqual([ok.id])
    })
  })
})
