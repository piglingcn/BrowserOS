/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * File-backed agent profile reader. Provisioning happens outside this
 * server now; this service only projects stored profiles for live tabs.
 */

import { logger } from '../../lib/logger'
import { listFiles, readJson } from '../../lib/storage'
import { publicMcpUrl } from '../../shared/mcp-url'
import {
  type AgentProfileSummary,
  type StoredAgentProfile,
  storedAgentProfileSchema,
} from './schemas'

const AGENTS_SUBDIR = 'agents'
const TOTAL_PROFILE_LOGINS = 47

/**
 * All readable stored profiles, in arbitrary order. A single corrupt
 * file is logged + skipped rather than rejecting the whole list.
 */
async function loadAll(): Promise<StoredAgentProfile[]> {
  const names = await listFiles(AGENTS_SUBDIR)
  const settled = await Promise.allSettled(
    names.map((name) =>
      readJson(`${AGENTS_SUBDIR}/${name}`, storedAgentProfileSchema),
    ),
  )
  const profiles: StoredAgentProfile[] = []
  for (let i = 0; i < settled.length; i++) {
    const result = settled[i]
    if (result.status === 'fulfilled') {
      profiles.push(result.value)
    } else {
      logger.warn('skipping unreadable agent profile', {
        file: `${AGENTS_SUBDIR}/${names[i]}`,
        error:
          result.reason instanceof Error
            ? result.reason.message
            : String(result.reason),
      })
    }
  }
  return profiles
}

function summariseProfile(profile: StoredAgentProfile): AgentProfileSummary {
  const blockedActionCount = Object.values(profile.approvals).filter(
    (verdict) => verdict === 'Block',
  ).length
  const loginCount =
    profile.loginMode === 'selective'
      ? profile.selectedSites.length
      : TOTAL_PROFILE_LOGINS
  const loginScopeLabel =
    profile.loginMode === 'selective'
      ? `Selective (${profile.selectedSites.length})`
      : profile.loginMode === 'all'
        ? `All my logins (${TOTAL_PROFILE_LOGINS})`
        : `Current profile (${TOTAL_PROFILE_LOGINS})`
  return {
    id: profile.id,
    name: profile.name,
    harness: profile.harness,
    loginScopeLabel,
    loginCount,
    aclRuleCount: profile.aclRuleIds.length,
    blockedActionCount,
    alwaysAllowCount: 0,
    lastRunAt: 'Never run',
    status: profile.status,
    mcpUrl: publicMcpUrl(),
  }
}

/** Returns the tab-activity projection of stored agent profiles. */
export async function list(): Promise<AgentProfileSummary[]> {
  const profiles = await loadAll()
  return profiles
    .sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : -1))
    .map((profile) => summariseProfile(profile))
}
