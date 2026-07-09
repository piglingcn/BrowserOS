/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { createTabGroupTracker } from './tracker'

export type { TabGroupColor } from './group-color'
export { hexForSlug } from './group-color'
export type { TabGroupRecord, TabGroupTracker } from './tracker'

/** Process-wide singleton consumed by the v2 dispatch path and the focus route. */
export const tabGroupTracker = createTabGroupTracker()
