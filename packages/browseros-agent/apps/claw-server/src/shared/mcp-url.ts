/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * Server-side MCP URL helpers. Browser/UI imports must use
 * mcp-url-common so env/config parsing stays out of the WXT bundle.
 */

import { env } from '../env'
import { canonicalMcpUrlForPort } from './mcp-url-common'

export {
  BROWSEROS_MCP_SERVER_NAME,
  canonicalMcpUrlForPort,
  MCP_PATH,
} from './mcp-url-common'

/** Resolves the public server-side MCP URL from runtime ports. */
export function publicMcpUrl(): string {
  return canonicalMcpUrlForPort(env.proxyPort ?? env.serverPort)
}
