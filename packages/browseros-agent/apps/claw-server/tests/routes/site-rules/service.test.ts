/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { describe, expect, test } from 'bun:test'
import * as siteRules from '../../../src/routes/site-rules/service'
import { writeSiteRules } from '../../_helpers/site-rules'
import { withTempBrowserClawDir } from '../../_helpers/temp-browserclaw-dir'

describe('site-rules service', () => {
  test('findMatching returns [] before any rule file exists', async () => {
    await withTempBrowserClawDir(async () => {
      expect(await siteRules.findMatching('mercury.com', 'payments')).toEqual(
        [],
      )
    })
  })

  test('findMatching honours glob patterns end-to-end', async () => {
    await withTempBrowserClawDir(async () => {
      await writeSiteRules([
        {
          label: 'Wire',
          domain: 'mercury.com',
          action: 'payments',
        },
        {
          label: 'Admin',
          domain: 'admin.*',
          action: 'admin',
        },
        {
          label: 'Stripe',
          domain: '*.stripe.com',
          action: 'payments',
        },
      ])

      const exact = await siteRules.findMatching('mercury.com', 'payments')
      expect(exact.map((rule) => rule.label)).toEqual(['Wire'])

      const sub = await siteRules.findMatching('api.stripe.com', 'payments')
      expect(sub.map((rule) => rule.label)).toEqual(['Stripe'])

      const admin = await siteRules.findMatching('admin.example.com', 'admin')
      expect(admin.map((rule) => rule.label)).toEqual(['Admin'])

      const wrongAction = await siteRules.findMatching('mercury.com', 'submit')
      expect(wrongAction).toEqual([])

      const noMatch = await siteRules.findMatching('elsewhere.org', 'submit')
      expect(noMatch).toEqual([])
    })
  })
})
