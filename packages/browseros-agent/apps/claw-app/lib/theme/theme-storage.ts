export type Theme = 'light' | 'dark' | 'system'

export const THEME_STORAGE_KEY = 'claw:theme'

const themes: readonly Theme[] = ['light', 'dark', 'system']

export function normalizeTheme(value: unknown): Theme {
  return themes.includes(value as Theme) ? (value as Theme) : 'system'
}

/**
 * localStorage over chrome.storage so the theme also works when the
 * build is served as a plain web page (dev:web); absent in bun tests
 * and may throw in sandboxed frames, hence the guard.
 */
function safeStorage(): Pick<Storage, 'getItem' | 'setItem'> | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

export function getStoredTheme(): Theme {
  return normalizeTheme(safeStorage()?.getItem(THEME_STORAGE_KEY))
}

export function setStoredTheme(theme: Theme): void {
  safeStorage()?.setItem(THEME_STORAGE_KEY, theme)
}

/** Resolves a preference to a concrete scheme; system consults the OS. */
export function resolveTheme(theme: Theme): 'light' | 'dark' {
  if (theme !== 'system') return theme
  return typeof window !== 'undefined' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light'
}
