/**
 * @license
 * Copyright 2026 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import type { ReplayEvent, ReplayFrame } from '@/modules/api/replay.hooks'

/**
 * A single tab's replay view. All fields are scoped to one
 * `tabPageId`:
 *   - `frames`: filtered AND time-shifted so `t=0` is the tab's
 *     first activity, not session start.
 *   - `events`: pass-through. rrweb treats the first event's `ts`
 *     as the tab-relative playback origin.
 *   - `totalSeconds`: duration of this tab's activity window (from
 *     first event to last event). 0 for empty tabs.
 */
export interface TabView {
  frames: ReplayFrame[]
  events: readonly ReplayEvent[]
  totalSeconds: number
}

export const EMPTY_TAB_VIEW: TabView = {
  frames: [],
  events: [],
  totalSeconds: 0,
}

export interface BuildTabViewInput {
  /** Session-scoped frames (any pageId). */
  frames: ReplayFrame[]
  eventsForTab: (tabPageId: number) => readonly ReplayEvent[]
  /** Session start in ms since epoch. Anchor for frame `t` values. */
  startedAtMs: number
}

/** Builds the frame/event/duration view for the selected replay tab. */
export function buildTabView(
  input: BuildTabViewInput,
  tabPageId: number | null,
): TabView {
  if (tabPageId === null) return EMPTY_TAB_VIEW
  const rawFrames = input.frames.filter((f) => f.pageId === tabPageId)
  const events = input.eventsForTab(tabPageId)
  if (rawFrames.length === 0 && events.length === 0) return EMPTY_TAB_VIEW
  const startedMs = input.startedAtMs
  const originMs =
    events.length > 0
      ? events[0]?.ts
      : startedMs + (rawFrames[0]?.t ?? 0) * 1000
  const endMs =
    events.length > 0
      ? events[events.length - 1]?.ts
      : startedMs + (rawFrames[rawFrames.length - 1]?.t ?? 0) * 1000
  const totalSeconds = Math.max(0, (endMs - originMs) / 1000)
  const originT = (originMs - startedMs) / 1000
  const frames = rawFrames.map((f) => ({
    ...f,
    t: Math.max(0, f.t - originT),
  }))
  return { frames, events, totalSeconds }
}
