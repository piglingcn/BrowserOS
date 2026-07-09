/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * Tests for the request-failure log middleware in src/server.ts.
 * Every >=400 response must produce exactly one structured
 * 'request failed' line (warn for 4xx, error for 5xx) regardless of
 * whether the failure was a router 404, a thrown HttpError, or an
 * unhandled error resolved by `app.onError`; sub-400 traffic stays
 * unlogged so polling endpoints cannot flood the rotating log file.
 *
 * The thrown-error paths run on a fixture app wired like server.ts
 * (same middleware + an HttpError-aware onError): the shared app's
 * route matcher is already built once any test file has fetched
 * through it, so throw-only routes cannot be mounted there.
 */

import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test'
import { Hono } from 'hono'
import { HttpError } from '../../src/lib/errors'
import { logger } from '../../src/lib/logger'
import app, { requestFailureLog } from '../../src/server'

let warnSpy: ReturnType<typeof spyOn<typeof logger, 'warn'>>
let errorSpy: ReturnType<typeof spyOn<typeof logger, 'error'>>

beforeEach(() => {
  warnSpy = spyOn(logger, 'warn')
  errorSpy = spyOn(logger, 'error')
})

afterEach(() => {
  warnSpy.mockRestore()
  errorSpy.mockRestore()
})

describe('request-failure logging on the live app', () => {
  test('successful responses log nothing', async () => {
    const res = await app.fetch(new Request('http://localhost/system/health'))
    expect(res.status).toBe(200)
    expect(warnSpy).not.toHaveBeenCalled()
    expect(errorSpy).not.toHaveBeenCalled()
  })

  test('router 404 logs one warn with method, path, status, duration', async () => {
    const res = await app.fetch(new Request('http://localhost/__no-such-route'))
    expect(res.status).toBe(404)
    expect(warnSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy).not.toHaveBeenCalled()
    const [msg, fields] = warnSpy.mock.calls[0] ?? []
    expect(msg).toBe('request failed')
    expect(fields).toMatchObject({
      method: 'GET',
      path: '/__no-such-route',
      status: 404,
    })
    expect(fields?.durationMs).toBeGreaterThanOrEqual(0)
  })
})

describe('request-failure logging with thrown errors (fixture app)', () => {
  // Mirrors server.ts's composition: middleware before routes, and an
  // onError that maps HttpError to its status and everything else to
  // a 500 — so the middleware sees the same final statuses it would
  // on the real app.
  function fixtureApp(): Hono {
    const fx = new Hono()
    fx.onError((err, c) => {
      if (err instanceof HttpError) {
        return c.json({ error: err.message }, err.status as 400 | 404 | 409)
      }
      return c.json({ error: 'internal error' }, 500)
    })
    fx.use('*', requestFailureLog)
    fx.get('/boom', () => {
      throw new Error('boom')
    })
    fx.get('/conflict', () => {
      throw new HttpError(409, 'already exists')
    })
    fx.get('/direct', (c) => c.json({ error: 'gone' }, 410))
    fx.get('/ok', (c) => c.json({ ok: true }))
    return fx
  }

  test('unhandled error logs one error line with status 500', async () => {
    const res = await fixtureApp().fetch(new Request('http://localhost/boom'))
    expect(res.status).toBe(500)
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(warnSpy).not.toHaveBeenCalled()
    const [msg, fields] = errorSpy.mock.calls[0] ?? []
    expect(msg).toBe('request failed')
    expect(fields).toMatchObject({
      method: 'GET',
      path: '/boom',
      status: 500,
    })
    expect(fields?.durationMs).toBeGreaterThanOrEqual(0)
  })

  test('thrown HttpError logs one warn with its status', async () => {
    const res = await fixtureApp().fetch(
      new Request('http://localhost/conflict'),
    )
    expect(res.status).toBe(409)
    expect(await res.json()).toEqual({ error: 'already exists' })
    expect(warnSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy).not.toHaveBeenCalled()
    expect(warnSpy.mock.calls[0]?.[1]).toMatchObject({
      method: 'GET',
      path: '/conflict',
      status: 409,
    })
  })

  test('direct 4xx JSON return logs one warn with its status', async () => {
    const res = await fixtureApp().fetch(new Request('http://localhost/direct'))
    expect(res.status).toBe(410)
    expect(warnSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy).not.toHaveBeenCalled()
    expect(warnSpy.mock.calls[0]?.[1]).toMatchObject({
      method: 'GET',
      path: '/direct',
      status: 410,
    })
  })

  test('sub-400 responses log nothing', async () => {
    const res = await fixtureApp().fetch(new Request('http://localhost/ok'))
    expect(res.status).toBe(200)
    expect(warnSpy).not.toHaveBeenCalled()
    expect(errorSpy).not.toHaveBeenCalled()
  })
})
