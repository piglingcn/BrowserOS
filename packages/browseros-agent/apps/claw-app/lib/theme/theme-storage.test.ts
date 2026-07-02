import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import {
  getStoredTheme,
  normalizeTheme,
  resolveTheme,
  setStoredTheme,
  THEME_STORAGE_KEY,
} from './theme-storage'

class MemoryStorage {
  private store = new Map<string, string>()

  getItem(key: string): string | null {
    return this.store.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.store.set(key, value)
  }

  removeItem(key: string): void {
    this.store.delete(key)
  }
}

const globals = globalThis as { localStorage?: unknown }

describe('theme-storage', () => {
  beforeEach(() => {
    globals.localStorage = new MemoryStorage()
  })

  afterEach(() => {
    delete globals.localStorage
  })

  it('round-trips each theme through set/get', () => {
    for (const theme of ['light', 'dark', 'system'] as const) {
      setStoredTheme(theme)
      expect(getStoredTheme()).toBe(theme)
    }
  })

  it('persists under the claw:theme key', () => {
    setStoredTheme('dark')
    expect(
      (globals.localStorage as MemoryStorage).getItem(THEME_STORAGE_KEY),
    ).toBe('dark')
  })

  it('falls back to system when the key is unset', () => {
    expect(getStoredTheme()).toBe('system')
  })

  it('falls back to system for junk stored values', () => {
    for (const junk of ['blue', '', 'DARK']) {
      ;(globals.localStorage as MemoryStorage).setItem(THEME_STORAGE_KEY, junk)
      expect(getStoredTheme()).toBe('system')
    }
  })

  it('is safe without a localStorage global', () => {
    delete globals.localStorage
    expect(getStoredTheme()).toBe('system')
    expect(() => setStoredTheme('dark')).not.toThrow()
  })

  it('normalizes arbitrary values', () => {
    expect(normalizeTheme('dark')).toBe('dark')
    expect(normalizeTheme('light')).toBe('light')
    expect(normalizeTheme(null)).toBe('system')
    expect(normalizeTheme(42)).toBe('system')
  })

  it('resolves explicit themes as-is and system to light without a window', () => {
    expect(resolveTheme('dark')).toBe('dark')
    expect(resolveTheme('light')).toBe('light')
    expect(resolveTheme('system')).toBe('light')
  })
})
