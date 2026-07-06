/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * File-backed agent profile reader. Provisioning happens outside this
 * server now; this service only projects stored profiles for live tabs
 * and permission checks.
 */

import { logger } from '../../lib/logger'
import { listFiles, readJson } from '../../lib/storage'
import { publicMcpUrl } from '../../shared/mcp-url'
import {
  type AgentProfileDetail,
  type AgentProfileSummary,
  type StoredAgentProfile,
  storedAgentProfileSchema,
} from './schemas'

const AGENTS_SUBDIR = 'agents'
const TOTAL_PROFILE_LOGINS = 47
const ID_PATTERN = /^[A-Za-z0-9_-]+$/
const MAX_ID_LENGTH = 64

function isValidId(id: string): boolean {
  return id.length > 0 && id.length <= MAX_ID_LENGTH && ID_PATTERN.test(id)
}

function fileFor(id: string): string {
  return `${AGENTS_SUBDIR}/${id}.json`
}

/**
 * All readable stored profiles, in arbitrary order. A single corrupt
 * file is logged + skipped rather than rejecting the whole list, so
 * one bad agent json can't brick the homepage or permission checks.
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

/** Stored profile for an id, or null when the file is missing. */
async function loadById(id: string): Promise<StoredAgentProfile | null> {
  if (!isValidId(id)) return null
  try {
    return await readJson(fileFor(id), storedAgentProfileSchema)
  } catch (err) {
    if (
      err instanceof Error &&
      (err.name === 'StorageNotFoundError' ||
        err.name === 'StorageInvalidPathError')
    ) {
      return null
    }
    throw err
  }
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

function stripManagedFields(profile: StoredAgentProfile): AgentProfileDetail {
  return {
    name: profile.name,
    harness: profile.harness,
    loginMode: profile.loginMode,
    selectedSites: [...profile.selectedSites],
    approvals: { ...profile.approvals },
    aclRuleIds: [...profile.aclRuleIds],
    customAclRules: profile.customAclRules.map((rule) => ({ ...rule })),
  }
}

/** Returns the tab-activity projection of stored agent profiles. */
export async function list(): Promise<AgentProfileSummary[]> {
  const profiles = await loadAll()
  return profiles
    .sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : -1))
    .map((profile) => summariseProfile(profile))
}

/** Returns profile settings for permission checks, excluding managed fields. */
export async function getDetail(
  id: string,
): Promise<AgentProfileDetail | null> {
  const profile = await loadById(id)
  return profile ? stripManagedFields(profile) : null
}
