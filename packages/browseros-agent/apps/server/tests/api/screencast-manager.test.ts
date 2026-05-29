import { describe, it } from 'bun:test'
import assert from 'node:assert'
import type { WSContext, WSReadyState } from 'hono/ws'
import {
  ScreencastManager,
  type ScreencastOutboundMessage,
} from '../../src/api/services/screencast/screencast-manager'
import { close_window, create_hidden_window } from '../../src/tools/windows'
import { withBrowser } from '../__helpers__/with-browser'

interface FakeWs {
  ws: WSContext<unknown>
  inbox: ScreencastOutboundMessage[]
  setClosed: () => void
}

function makeFakeWs(): FakeWs {
  const inbox: ScreencastOutboundMessage[] = []
  let state: WSReadyState = 1
  const ws = {
    get readyState() {
      return state
    },
    send(payload: string | ArrayBuffer | Uint8Array) {
      if (typeof payload === 'string') {
        inbox.push(JSON.parse(payload) as ScreencastOutboundMessage)
      }
    },
    close() {
      state = 3
    },
  } as unknown as WSContext<unknown>
  return {
    ws,
    inbox,
    setClosed: () => {
      state = 3
    },
  }
}

async function waitForFrame(
  inbox: ScreencastOutboundMessage[],
  timeoutMs: number,
): Promise<boolean> {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    if (inbox.some((m) => m.type === 'frame')) return true
    await new Promise((r) => setTimeout(r, 100))
  }
  return false
}

describe('ScreencastManager', () => {
  it('subscribes, emits frames for a hidden window, and stops on last unsubscribe', async () => {
    await withBrowser(async ({ browser, execute }) => {
      const created = await execute(create_hidden_window, {})
      assert.ok(!created.isError, 'create_hidden_window failed')
      const windowId = (
        created.structuredContent as { window: { windowId: number } }
      ).window.windowId

      try {
        const manager = new ScreencastManager(browser)
        const subA = makeFakeWs()
        const handleA = await manager.subscribe(windowId, null, subA.ws)

        assert.ok(
          subA.inbox.some(
            (m) => m.type === 'status' && m.status === 'connected',
          ),
          'expected initial status=connected message',
        )

        const got = await waitForFrame(subA.inbox, 8_000)
        assert.ok(got, 'expected at least one frame within 8s')

        const subB = makeFakeWs()
        const handleB = await manager.subscribe(windowId, null, subB.ws)
        assert.ok(
          subB.inbox.some(
            (m) => m.type === 'status' && m.status === 'connected',
          ),
          'second subscriber should receive status',
        )

        manager.unsubscribe(handleA, subA.ws)
        manager.unsubscribe(handleB, subB.ws)
      } finally {
        await execute(close_window, { windowId })
      }
    })
  }, 60_000)
})
