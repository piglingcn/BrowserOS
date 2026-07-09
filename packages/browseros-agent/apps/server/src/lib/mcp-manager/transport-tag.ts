/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { ensureClaudeCodeHttpTransportTag as ensureSharedClaudeCodeHttpTransportTag } from '@browseros/shared/mcp/claude-code-transport-tag'
import { resolveAgentMcpConfigPath } from 'agent-mcp-manager'
import { logger } from '../logger'
import { BROWSEROS_MCP_SERVER_NAME } from './manager'

export interface EnsureClaudeCodeHttpTransportTagOptions {
  configPath?: string
  serverName?: string
}

export async function ensureClaudeCodeHttpTransportTag(
  options: EnsureClaudeCodeHttpTransportTagOptions = {},
): Promise<boolean> {
  const serverName = options.serverName ?? BROWSEROS_MCP_SERVER_NAME
  try {
    const configPath =
      options.configPath ??
      (await resolveAgentMcpConfigPath('claude-code', 'system'))
    return await ensureSharedClaudeCodeHttpTransportTag({
      configPath,
      serverName,
      logger,
    })
  } catch (err) {
    logger.warn('Failed to ensure Claude Code MCP transport tag', {
      serverName,
      error: err instanceof Error ? err.message : String(err),
    })
    return false
  }
}
