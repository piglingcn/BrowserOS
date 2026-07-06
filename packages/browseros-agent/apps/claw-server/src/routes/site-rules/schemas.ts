/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { z } from 'zod'

const siteRuleActionEnum = z.enum([
  'payments',
  'submit',
  'delete',
  'navigate',
  'upload',
  'admin',
])
export type SiteRuleAction = z.infer<typeof siteRuleActionEnum>

const siteRuleSchema = z.object({
  id: z.string(),
  label: z.string().min(1),
  domain: z.string().min(1),
  action: siteRuleActionEnum,
})
export type SiteRule = z.infer<typeof siteRuleSchema>

export const siteRulesFileSchema = z.array(siteRuleSchema)
