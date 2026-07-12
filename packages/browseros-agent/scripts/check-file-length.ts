import { existsSync, readFileSync } from 'node:fs'

const MAX_LINES = 400

for (const file of process.argv.slice(2)) {
  if (!existsSync(file)) continue

  const lineCount = readFileSync(file, 'utf8').match(/\n/g)?.length ?? 0
  if (lineCount <= MAX_LINES) continue

  console.warn(
    `Warning: ${file} has ${lineCount} lines (threshold: ${MAX_LINES})`,
  )
  console.warn(
    'Consider splitting this file if it has multiple responsibilities.',
  )
}
