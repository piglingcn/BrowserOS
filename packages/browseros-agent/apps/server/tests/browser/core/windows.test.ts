import { describe, expect, it } from 'bun:test'
import { Browser } from '../../../src/browser/browser'
import type { CdpConnection } from '../../../src/browser/core/connection'
import {
  type WindowInfo,
  WindowManager,
} from '../../../src/browser/core/windows'

function makeWindow(
  windowId: number,
  overrides: Partial<WindowInfo> = {},
): WindowInfo {
  return {
    windowId,
    windowType: 'normal',
    bounds: {},
    isActive: false,
    isVisible: true,
    tabCount: 1,
    ...overrides,
  }
}

function createConnection() {
  const calls: Array<{ method: string; params?: unknown }> = []
  const windows = [makeWindow(1, { isActive: true })]

  const connection = {
    Browser: {
      getWindows: async () => {
        calls.push({ method: 'getWindows' })
        return { windows }
      },
      createWindow: async (params?: { hidden?: boolean }) => {
        calls.push({ method: 'createWindow', params })
        return {
          window: makeWindow(2, { isVisible: params?.hidden !== true }),
        }
      },
      closeWindow: async (params: { windowId: number }) => {
        calls.push({ method: 'closeWindow', params })
      },
      activateWindow: async (params: { windowId: number }) => {
        calls.push({ method: 'activateWindow', params })
      },
      setWindowVisibility: async (params: {
        windowId: number
        visible: boolean
        activate?: boolean
      }) => {
        calls.push({ method: 'setWindowVisibility', params })
        return {
          previousWindowId: params.windowId,
          replaced: true,
          window: makeWindow(3, { isVisible: params.visible }),
        }
      },
    },
    Target: {
      on: () => () => {},
    },
    isConnected: () => true,
    connectionEpoch: () => 0,
    session: () => ({}),
  } as unknown as CdpConnection

  return { connection, calls, windows }
}

describe('WindowManager', () => {
  it('delegates window operations to the Browser CDP domain', async () => {
    const { connection, calls, windows } = createConnection()
    const manager = new WindowManager(connection)

    await expect(manager.list()).resolves.toEqual(windows)
    await expect(manager.create({ hidden: true })).resolves.toMatchObject({
      windowId: 2,
      isVisible: false,
    })
    await manager.close(7)
    await manager.activate(8)
    await expect(
      manager.setVisibility(9, { visible: true, activate: false }),
    ).resolves.toMatchObject({
      previousWindowId: 9,
      newWindowId: 3,
      replaced: true,
      window: { windowId: 3, isVisible: true },
    })

    expect(calls).toEqual([
      { method: 'getWindows' },
      { method: 'createWindow', params: { hidden: true } },
      { method: 'closeWindow', params: { windowId: 7 } },
      { method: 'activateWindow', params: { windowId: 8 } },
      {
        method: 'setWindowVisibility',
        params: { windowId: 9, visible: true, activate: false },
      },
    ])
  })
})

describe('Browser window facade', () => {
  it('uses the browser-core window manager', async () => {
    const { connection, calls } = createConnection()
    const browser = new Browser(connection as never)

    await browser.listWindows()
    await browser.createWindow({ hidden: true })
    await browser.closeWindow(4)
    await browser.activateWindow(5)
    await expect(
      browser.setWindowVisibility(6, { visible: false }),
    ).resolves.toMatchObject({
      previousWindowId: 6,
      newWindowId: 3,
      window: { isVisible: false },
    })

    expect(calls).toEqual([
      { method: 'getWindows' },
      { method: 'createWindow', params: { hidden: true } },
      { method: 'closeWindow', params: { windowId: 4 } },
      { method: 'activateWindow', params: { windowId: 5 } },
      {
        method: 'setWindowVisibility',
        params: { windowId: 6, visible: false },
      },
    ])
  })
})
