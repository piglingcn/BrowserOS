/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { ensureClaudeCodeHttpTransportTag } from '@browseros/shared/mcp/claude-code-transport-tag'
import type { LoggerInterface } from '@browseros/shared/types/logger'
import { resolveAgentMcpConfigPath } from 'agent-mcp-manager'
import { logger } from '../lib/logger'

const CLAUDE_CODE_BROWSEROS_SERVER_NAMES = ['BrowserClaw', 'browseros'] as const
const LOCAL_BROWSEROS_MCP_URL = /^http:\/\/127\.0\.0\.1:\d+\/mcp$/

export interface HealClaudeCodeBrowserOsHttpTransportTagsOptions {
  configPath?: string
  resolveConfigPath?: typeof resolveAgentMcpConfigPath
  logger?: LoggerInterface
}

export async function healClaudeCodeBrowserOsHttpTransportTags(
  options: HealClaudeCodeBrowserOsHttpTransportTagsOptions = {},
): Promise<number> {
  const log = options.logger ?? logger
  try {
    const configPath =
      options.configPath ??
      (await (options.resolveConfigPath ?? resolveAgentMcpConfigPath)(
        'claude-code',
        'system',
      ))
    let healed = 0
    for (const serverName of CLAUDE_CODE_BROWSEROS_SERVER_NAMES) {
      const changed = await ensureClaudeCodeHttpTransportTag({
        configPath,
        serverName,
        expectedUrlPattern: LOCAL_BROWSEROS_MCP_URL,
        onlyIfMissing: true,
        logger: log,
      })
      if (changed) healed++
    }
    return healed
  } catch (err) {
    log.warn('Failed to heal Claude Code BrowserOS MCP transport tags', {
      error: err instanceof Error ? err.message : String(err),
    })
    return 0
  }
}
