import { writeTempToolOutputFile } from './output-file'
import { estimateTextTokens } from './token-estimate'
import { wrapUntrusted } from './trust-boundary'

const LARGE_SNAPSHOT_TOKEN_THRESHOLD = 15_000
const MAX_SAVE_FAILURE_EXCERPT_CHARS = 4_000

export interface FormattedSnapshot {
  text: string
  structured?: Record<string, unknown>
}

/** Formats page snapshots for direct tools and automatic post-action readback. */
export async function formatSnapshotResult(
  snapshot: string,
  origin: string,
): Promise<FormattedSnapshot> {
  const snapshotText = snapshot || '(empty page)'
  const wrappedSnapshot = wrapUntrusted(snapshotText, origin)
  const contentLength = wrappedSnapshot.length
  const tokenEstimate = estimateTextTokens(wrappedSnapshot)

  if (tokenEstimate > LARGE_SNAPSHOT_TOKEN_THRESHOLD) {
    try {
      const path = await writeTempToolOutputFile({
        toolName: 'snapshot',
        extension: 'md',
        content: wrappedSnapshot,
      })

      return {
        text: [
          `Large snapshot (${tokenEstimate} estimated tokens, ${contentLength} chars) saved to: ${path}`,
          'Read the file for the full snapshot and refs.',
        ].join('\n'),
        structured: {
          snapshot: wrappedSnapshot,
          path,
          contentLength,
          tokenEstimate,
          writtenToFile: true,
        },
      }
    } catch (error) {
      const saveError = error instanceof Error ? error.message : String(error)
      const excerpt = snapshotText.slice(0, MAX_SAVE_FAILURE_EXCERPT_CHARS)
      return {
        text: [
          `Large snapshot (${tokenEstimate} estimated tokens, ${contentLength} chars) could not be saved to a BrowserOS output file: ${saveError}`,
          `Showing the first ${excerpt.length} chars instead:`,
          wrapUntrusted(excerpt, origin),
        ].join('\n'),
        structured: {
          snapshot: wrappedSnapshot,
          contentLength,
          tokenEstimate,
          writtenToFile: false,
          outputWriteFailed: true,
          error: saveError,
        },
      }
    }
  }

  return { text: wrappedSnapshot, structured: { snapshot: wrappedSnapshot } }
}
