/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { describe, expect, test } from 'bun:test'
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { healClaudeCodeBrowserOsHttpTransportTags } from '../../src/services/claude-code-heal'

async function withTempConfig<T>(
  run: (path: string) => Promise<T>,
): Promise<T> {
  const dir = await mkdtemp(join(tmpdir(), 'claw-claude-code-heal-'))
  try {
    return await run(join(dir, '.claude.json'))
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
}

describe('healClaudeCodeBrowserOsHttpTransportTags', () => {
  test('adds type http to both known BrowserOS local URL entries without removing either name', async () => {
    await withTempConfig(async (configPath) => {
      const before = `{
  "theme": "dark",
  "mcpServers": {
    "browseros": {
      "url": "http://127.0.0.1:9100/mcp"
    },
    "BrowserClaw": {
      "url": "http://127.0.0.1:9200/mcp"
    },
    "browseros-stdio": {
      "command": "npx",
      "args": ["mcp-remote", "http://127.0.0.1:9200/mcp"]
    },
    "other": {
      "url": "http://127.0.0.1:9200/mcp"
    }
  },
  "history": ["keep"]
}
`
      const expected = `{
  "theme": "dark",
  "mcpServers": {
    "browseros": {
      "url": "http://127.0.0.1:9100/mcp",
      "type": "http"
    },
    "BrowserClaw": {
      "url": "http://127.0.0.1:9200/mcp",
      "type": "http"
    },
    "browseros-stdio": {
      "command": "npx",
      "args": ["mcp-remote", "http://127.0.0.1:9200/mcp"]
    },
    "other": {
      "url": "http://127.0.0.1:9200/mcp"
    }
  },
  "history": ["keep"]
}
`
      await writeFile(configPath, before, 'utf8')

      await expect(
        healClaudeCodeBrowserOsHttpTransportTags({ configPath }),
      ).resolves.toBe(2)

      const after = await readFile(configPath, 'utf8')
      expect(after).toBe(expected)
      const parsed = JSON.parse(after)
      expect(Object.keys(parsed.mcpServers)).toEqual([
        'browseros',
        'BrowserClaw',
        'browseros-stdio',
        'other',
      ])
      expect(parsed.mcpServers.browseros.type).toBe('http')
      expect(parsed.mcpServers.BrowserClaw.type).toBe('http')
      expect(parsed.mcpServers['browseros-stdio']).toEqual({
        command: 'npx',
        args: ['mcp-remote', 'http://127.0.0.1:9200/mcp'],
      })
      expect(parsed.mcpServers.other).toEqual({
        url: 'http://127.0.0.1:9200/mcp',
      })
    })
  })

  test('does not write when known entries are already correct', async () => {
    await withTempConfig(async (configPath) => {
      const source = `{
  "mcpServers": {
    "BrowserClaw": {
      "type": "http",
      "url": "http://127.0.0.1:9200/mcp"
    },
    "browseros": {
      "url": "http://127.0.0.1:9100/mcp",
      "type": "http"
    }
  }
}
`
      await writeFile(configPath, source, 'utf8')
      const beforeStat = await stat(configPath)

      await new Promise((resolve) => setTimeout(resolve, 20))
      await expect(
        healClaudeCodeBrowserOsHttpTransportTags({ configPath }),
      ).resolves.toBe(0)

      await expect(readFile(configPath, 'utf8')).resolves.toBe(source)
      expect((await stat(configPath)).mtimeMs).toBe(beforeStat.mtimeMs)
    })
  })

  test('ignores foreign names, non-local URLs, and entries that already declare a type', async () => {
    await withTempConfig(async (configPath) => {
      const source = `{
  "mcpServers": {
    "ForeignClaw": {
      "url": "http://127.0.0.1:9200/mcp"
    },
    "BrowserClaw": {
      "url": "https://example.com/mcp"
    },
    "browseros": {
      "type": "sse",
      "url": "http://127.0.0.1:9100/mcp"
    },
    "browseros-stdio": {
      "command": "npx"
    }
  }
}
`
      await writeFile(configPath, source, 'utf8')

      await expect(
        healClaudeCodeBrowserOsHttpTransportTags({ configPath }),
      ).resolves.toBe(0)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(source)
    })
  })
})
