declare const __BROWSEROS_VERSION__: string

export const VERSION: string =
  typeof __BROWSEROS_VERSION__ !== 'undefined'
    ? __BROWSEROS_VERSION__
    : '0.0.0-dev'
