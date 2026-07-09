/**
 * @license
 * Copyright 2025 BrowserOS
 */

import { describe, expect, it } from 'bun:test'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { ensureClaudeCodeHttpTransportTag as ensureSharedClaudeCodeHttpTransportTag } from '@browseros/shared/mcp/claude-code-transport-tag'
import { ensureClaudeCodeHttpTransportTag } from '../../../src/lib/mcp-manager/transport-tag'

async function withTempConfig<T>(
  run: (path: string) => Promise<T>,
): Promise<T> {
  const dir = await mkdtemp(join(tmpdir(), 'claude-transport-tag-'))
  try {
    return await run(join(dir, '.claude.json'))
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
}

describe('ensureClaudeCodeHttpTransportTag', () => {
  it('surgically adds type http to the BrowserOS claude-code entry', async () => {
    await withTempConfig(async (configPath) => {
      const before = `{
  "theme": "dark",
  "mcpServers": {
    "other": {
      "command": "node"
    },
    "browseros": {
      "url": "http://127.0.0.1:9100/mcp"
    }
  },
  "history": ["keep"]
}
`
      const expected = `{
  "theme": "dark",
  "mcpServers": {
    "other": {
      "command": "node"
    },
    "browseros": {
      "url": "http://127.0.0.1:9100/mcp",
      "type": "http"
    }
  },
  "history": ["keep"]
}
`
      await writeFile(configPath, before, 'utf8')

      await expect(
        ensureClaudeCodeHttpTransportTag({ configPath }),
      ).resolves.toBe(true)

      const after = await readFile(configPath, 'utf8')
      expect(after).toBe(expected)
      expect(JSON.parse(after)).toEqual({
        theme: 'dark',
        mcpServers: {
          other: { command: 'node' },
          browseros: {
            url: 'http://127.0.0.1:9100/mcp',
            type: 'http',
          },
        },
        history: ['keep'],
      })

      await expect(
        ensureClaudeCodeHttpTransportTag({ configPath }),
      ).resolves.toBe(false)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(expected)
    })
  })

  it('no-ops when the BrowserOS entry is missing', async () => {
    await withTempConfig(async (configPath) => {
      const source = `{
  "mcpServers": {
    "other": {
      "url": "http://127.0.0.1:9100/mcp"
    }
  }
}
`
      await writeFile(configPath, source, 'utf8')

      await expect(
        ensureClaudeCodeHttpTransportTag({ configPath }),
      ).resolves.toBe(false)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(source)
    })
  })

  it('no-ops when the config file is missing', async () => {
    await withTempConfig(async (configPath) => {
      await expect(
        ensureClaudeCodeHttpTransportTag({ configPath }),
      ).resolves.toBe(false)
    })
  })

  it('shared utility requires the expected URL pattern when supplied', async () => {
    await withTempConfig(async (configPath) => {
      const before = `{
  "mcpServers": {
    "BrowserClaw": {
      "url": "https://example.com/mcp"
    }
  }
}
`
      const expected = `{
  "mcpServers": {
    "BrowserClaw": {
      "url": "http://127.0.0.1:9200/mcp",
      "type": "http"
    }
  }
}
`
      await writeFile(configPath, before, 'utf8')

      await expect(
        ensureSharedClaudeCodeHttpTransportTag({
          configPath,
          serverName: 'BrowserClaw',
          expectedUrlPattern: /^http:\/\/127\.0\.0\.1:\d+\/mcp$/,
        }),
      ).resolves.toBe(false)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(before)

      await writeFile(
        configPath,
        before.replace('https://example.com/mcp', 'http://127.0.0.1:9200/mcp'),
        'utf8',
      )
      await expect(
        ensureSharedClaudeCodeHttpTransportTag({
          configPath,
          serverName: 'BrowserClaw',
          expectedUrlPattern: /^http:\/\/127\.0\.0\.1:\d+\/mcp$/,
        }),
      ).resolves.toBe(true)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(expected)
    })
  })

  it('shared utility can add only missing type fields', async () => {
    await withTempConfig(async (configPath) => {
      const source = `{
  "mcpServers": {
    "BrowserClaw": {
      "type": "sse",
      "url": "http://127.0.0.1:9200/mcp"
    }
  }
}
`
      await writeFile(configPath, source, 'utf8')

      await expect(
        ensureSharedClaudeCodeHttpTransportTag({
          configPath,
          serverName: 'BrowserClaw',
          onlyIfMissing: true,
        }),
      ).resolves.toBe(false)
      await expect(readFile(configPath, 'utf8')).resolves.toBe(source)
    })
  })
})
