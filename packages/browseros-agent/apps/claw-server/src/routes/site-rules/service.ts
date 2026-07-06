/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * File-backed site-rules reader. Rule provisioning happens outside
 * this server now; permission checks only need read-time matching.
 */

import { matchDomain } from '../../lib/match-domain'
import { readJson, StorageNotFoundError } from '../../lib/storage'
import { type SiteRule, siteRulesFileSchema } from './schemas'

const FILE = 'site-rules.json'

async function loadAll(): Promise<SiteRule[]> {
  try {
    return await readJson(FILE, siteRulesFileSchema)
  } catch (err) {
    if (err instanceof StorageNotFoundError) return []
    throw err
  }
}

/**
 * In-process helper used by `permissions.check`. Returns every rule
 * whose `(domain, action)` pair matches the request. Caller decides
 * how to combine multiple matches (Phase 5: any match with a
 * configured verb yields a clamp).
 */
export async function findMatching(
  domain: string,
  action: SiteRule['action'],
): Promise<SiteRule[]> {
  const rules = await loadAll()
  return rules.filter(
    (rule) => rule.action === action && matchDomain(rule.domain, domain),
  )
}
