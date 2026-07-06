/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { writeJson } from '../../src/lib/storage'
import {
  type SiteRule,
  siteRulesFileSchema,
} from '../../src/routes/site-rules/schemas'

function slugForFixture(input: string): string {
  return input
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
}

export function makeSiteRule(overrides: Partial<SiteRule> = {}): SiteRule {
  const label = overrides.label ?? 'Rule'
  const domain = overrides.domain ?? 'example.com'
  const action = overrides.action ?? 'submit'
  return {
    id: overrides.id ?? slugForFixture(`${label}-${domain}-${action}`),
    label,
    domain,
    action,
  }
}

export async function writeSiteRules(
  rules: Array<Partial<SiteRule>>,
): Promise<SiteRule[]> {
  const rows = rules.map((rule) => makeSiteRule(rule))
  await writeJson('site-rules.json', rows, siteRulesFileSchema)
  return rows
}
