/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import type { ProtocolApi } from '@browseros/cdp-protocol/protocol-api'
import { SCREENCAST_LIMITS } from '@browseros/shared/constants/limits'
import type { WSContext } from 'hono/ws'
import type { Browser } from '../../../browser/browser'
import { logger } from '../../../lib/logger'

export interface ScreencastFrameMessage {
  type: 'frame'
  data: string
  metadata: {
    timestamp?: number
    deviceWidth?: number
    deviceHeight?: number
    offsetTop?: number
    pageScaleFactor?: number
    scrollOffsetX?: number
    scrollOffsetY?: number
  }
}

export interface ScreencastStatusMessage {
  type: 'status'
  status: 'connected' | 'detached'
  windowId: number
  pageId?: number
  url?: string
}

export type ScreencastOutboundMessage =
  | ScreencastFrameMessage
  | ScreencastStatusMessage

type Subscriber = WSContext<unknown>

interface ScreencastSession {
  targetId: string
  windowId: number
  pageId: number | null
  cdpSession: ProtocolApi
  subscribers: Set<Subscriber>
  unsubscribeFrame: () => void
  url: string
  // Chromium's Page.startScreencast only emits frames on compositor
  // invalidation. A static page produces one frame on attach and then
  // nothing — a late subscriber would see "live" status with a blank
  // canvas forever. Cache the last frame and replay it on subscribe.
  lastFrame: ScreencastFrameMessage | null
  // Set true once stopSession runs, so a concurrent subscribe()
  // continuation can detect that its session reference was torn down
  // mid-flight and retry against a fresh one.
  disposed: boolean
}

export interface SubscribeHandle {
  /** Pass back to `unsubscribe` so the manager doesn't have to re-resolve. */
  targetId: string
}

const WS_OPEN: 1 = 1

export class ScreencastManager {
  // Sessions keyed by targetId — the canonical CDP page identity. Both
  // windowId-only ("active page in this window") and explicit-pageId
  // subscribers resolve to a targetId before hitting this map, so they
  // share a session when they're really watching the same tab.
  private readonly sessions = new Map<string, ScreencastSession>()
  private readonly pendingStarts = new Map<string, Promise<ScreencastSession>>()

  constructor(private readonly browser: Browser) {}

  async subscribe(
    windowId: number,
    pageId: number | null,
    ws: Subscriber,
  ): Promise<SubscribeHandle> {
    // Retry loop: when two subscribers share a pendingStart and the
    // first one's ws closes mid-flight, its continuation runs first
    // and synchronously stops the session. The second continuation
    // would otherwise add ws to the disposed session — connected
    // status sent, but no live frames ever arrive (frame listener
    // already removed). Re-run getOrStartSession to bind to a fresh
    // session.
    for (;;) {
      const resolved = await this.resolve(windowId, pageId)
      const session = await this.getOrStartSession(resolved, windowId, pageId)
      // Route's onClose can fire while these awaits are in flight; it
      // sees `handle === null` and skips unsubscribe. Without this
      // guard we'd add a dead ws to subscribers and stopSession would
      // never run.
      if (ws.readyState !== WS_OPEN) {
        this.stopIfIdle(session)
        return { targetId: session.targetId }
      }
      if (session.disposed) continue
      session.subscribers.add(ws)
      this.send(ws, {
        type: 'status',
        status: 'connected',
        windowId,
        pageId: pageId ?? undefined,
        url: session.url,
      })
      if (session.lastFrame) {
        this.send(ws, session.lastFrame)
      } else {
        void this.primeWithScreenshot(session, ws).catch((err) => {
          logger.warn('primeWithScreenshot failed', {
            targetId: session.targetId,
            error: err instanceof Error ? err.message : String(err),
          })
        })
      }
      return { targetId: session.targetId }
    }
  }

  unsubscribe(handle: SubscribeHandle, ws: Subscriber): void {
    const session = this.sessions.get(handle.targetId)
    if (!session) return
    session.subscribers.delete(ws)
    this.stopIfIdle(session)
  }

  private stopIfIdle(session: ScreencastSession): void {
    if (session.subscribers.size > 0) return
    void this.stopSession(session.targetId).catch((err) => {
      logger.warn('Failed to stop idle screencast session', {
        targetId: session.targetId,
        error: err instanceof Error ? err.message : String(err),
      })
    })
  }

  private resolve(
    windowId: number,
    pageId: number | null,
  ): Promise<{ targetId: string; session: ProtocolApi; url: string }> {
    return pageId === null
      ? this.browser.getActivePageForWindow(windowId)
      : this.browser.getPageSession(pageId)
  }

  private async getOrStartSession(
    resolved: { targetId: string; session: ProtocolApi; url: string },
    windowId: number,
    pageId: number | null,
  ): Promise<ScreencastSession> {
    const existing = this.sessions.get(resolved.targetId)
    if (existing) return existing
    const pending = this.pendingStarts.get(resolved.targetId)
    if (pending) return pending
    const startPromise = this.startSession(resolved, windowId, pageId)
    this.pendingStarts.set(resolved.targetId, startPromise)
    try {
      const session = await startPromise
      this.sessions.set(resolved.targetId, session)
      return session
    } finally {
      this.pendingStarts.delete(resolved.targetId)
    }
  }

  private async startSession(
    resolved: { targetId: string; session: ProtocolApi; url: string },
    windowId: number,
    pageId: number | null,
  ): Promise<ScreencastSession> {
    // Page.enable was already called inside Browser.attachToPage;
    // startScreencast on a session without Page enabled is a silent
    // no-op, hence the ordering matters.
    await resolved.session.Page.startScreencast({
      format: 'jpeg',
      quality: SCREENCAST_LIMITS.DEFAULT_JPEG_QUALITY,
      everyNthFrame: SCREENCAST_LIMITS.EVERY_NTH_FRAME,
      maxWidth: SCREENCAST_LIMITS.MAX_WIDTH,
      maxHeight: SCREENCAST_LIMITS.MAX_HEIGHT,
    })
    const session: ScreencastSession = {
      targetId: resolved.targetId,
      windowId,
      pageId,
      cdpSession: resolved.session,
      subscribers: new Set(),
      url: resolved.url,
      unsubscribeFrame: () => undefined,
      lastFrame: null,
      disposed: false,
    }
    session.unsubscribeFrame = resolved.session.Page.on(
      'screencastFrame',
      (params) => {
        const frame: ScreencastFrameMessage = {
          type: 'frame',
          data: params.data,
          metadata: {
            timestamp: params.metadata.timestamp,
            deviceWidth: params.metadata.deviceWidth,
            deviceHeight: params.metadata.deviceHeight,
            offsetTop: params.metadata.offsetTop,
            pageScaleFactor: params.metadata.pageScaleFactor,
            scrollOffsetX: params.metadata.scrollOffsetX,
            scrollOffsetY: params.metadata.scrollOffsetY,
          },
        }
        session.lastFrame = frame
        this.broadcast(session, frame)
        resolved.session.Page.screencastFrameAck({
          sessionId: params.sessionId,
        }).catch((err) => {
          logger.warn('screencastFrameAck failed', {
            targetId: session.targetId,
            error: err instanceof Error ? err.message : String(err),
          })
        })
      },
    )
    return session
  }

  private async primeWithScreenshot(
    session: ScreencastSession,
    ws: Subscriber,
  ): Promise<void> {
    const result = await session.cdpSession.Page.captureScreenshot({
      format: 'jpeg',
      quality: SCREENCAST_LIMITS.DEFAULT_JPEG_QUALITY,
    })
    if (!result?.data) return
    const frame: ScreencastFrameMessage = {
      type: 'frame',
      data: result.data,
      metadata: {},
    }
    session.lastFrame = frame
    this.send(ws, frame)
  }

  private async stopSession(targetId: string): Promise<void> {
    const session = this.sessions.get(targetId)
    if (!session) return
    session.disposed = true
    this.sessions.delete(targetId)
    session.unsubscribeFrame()
    try {
      await session.cdpSession.Page.stopScreencast()
    } catch (err) {
      // The underlying target may already be gone (tab closed, page
      // navigated). Best-effort.
      logger.warn('stopScreencast threw', {
        targetId,
        error: err instanceof Error ? err.message : String(err),
      })
    }
  }

  private broadcast(
    session: ScreencastSession,
    message: ScreencastOutboundMessage,
  ): void {
    const payload = JSON.stringify(message)
    for (const ws of session.subscribers) {
      if (ws.readyState !== WS_OPEN) continue
      try {
        ws.send(payload)
      } catch (err) {
        logger.warn('Subscriber send failed; dropping subscriber', {
          targetId: session.targetId,
          error: err instanceof Error ? err.message : String(err),
        })
        session.subscribers.delete(ws)
      }
    }
    this.stopIfIdle(session)
  }

  private send(ws: Subscriber, message: ScreencastOutboundMessage): void {
    if (ws.readyState !== WS_OPEN) return
    try {
      ws.send(JSON.stringify(message))
    } catch {
      // Best-effort.
    }
  }
}
