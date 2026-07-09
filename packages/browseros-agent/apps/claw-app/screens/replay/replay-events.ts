/**
 * @license
 * Copyright 2026 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import type { ReplayEvent } from '@/modules/api/replay.hooks'

export const EMPTY_REPLAY_EVENTS: readonly ReplayEvent[] = []

export interface ReplayEventTabs {
  tabPageIds: number[]
  eventsForTab: (tabPageId: number) => readonly ReplayEvent[]
}

/** Groups rrweb events by tab while preserving each tab array's identity. */
export function buildReplayEventTabs(
  events: readonly ReplayEvent[],
): ReplayEventTabs {
  if (events.length === 0) {
    return {
      tabPageIds: [],
      eventsForTab: () => EMPTY_REPLAY_EVENTS,
    }
  }

  const tabPageIds: number[] = []
  const eventsByTab = new Map<number, ReplayEvent[]>()
  for (const event of events) {
    const list = eventsByTab.get(event.tabPageId)
    if (list) {
      list.push(event)
    } else {
      eventsByTab.set(event.tabPageId, [event])
      tabPageIds.push(event.tabPageId)
    }
  }

  return {
    tabPageIds,
    eventsForTab: (tabPageId) =>
      eventsByTab.get(tabPageId) ?? EMPTY_REPLAY_EVENTS,
  }
}
