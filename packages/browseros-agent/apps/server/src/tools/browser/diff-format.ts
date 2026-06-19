import type { SnapshotDiff } from '../../browser/core/snapshot/diff'
import { writeTempToolOutputFile } from './output-file'
import { estimateTextTokens } from './token-estimate'
import { wrapUntrusted } from './trust-boundary'

const MAX_INLINE_DIFF_TOKENS = 10_000
const MAX_SAVE_FAILURE_EXCERPT_CHARS = 4_000

export interface FormattedDiff {
  text: string
  structured?: Record<string, unknown>
}

/** Formats observer diffs for direct tools and automatic post-action readback. */
export async function formatDiffResult(
  diff: SnapshotDiff,
  origin: string,
): Promise<FormattedDiff> {
  if (!diff.changed) return { text: 'no change since last snapshot' }

  const diffText = diff.text || '(empty page)'
  const wrappedDiff = wrapUntrusted(diffText, origin)
  const tokenEstimate = estimateTextTokens(wrappedDiff)
  const structured = {
    added: diff.added,
    removed: diff.removed,
    ...(diff.urlChanged && {
      urlChanged: true,
      beforeUrl: diff.beforeUrl,
      afterUrl: diff.afterUrl,
    }),
    ...(diff.urlChanged ? { snapshot: wrappedDiff } : { diff: wrappedDiff }),
  }

  if (tokenEstimate > MAX_INLINE_DIFF_TOKENS) {
    try {
      const path = await writeTempToolOutputFile({
        toolName: 'diff',
        extension: 'md',
        content: wrappedDiff,
      })
      const text = diff.urlChanged
        ? `URL changed; full current snapshot is ${tokenEstimate} estimated tokens, over the ${MAX_INLINE_DIFF_TOKENS}-token inline limit, saved to: ${path}\nRead the file for the full current snapshot.`
        : `Diff is ${tokenEstimate} estimated tokens, over the ${MAX_INLINE_DIFF_TOKENS}-token inline limit, saved to: ${path}\nRead the file for the full diff.`
      return {
        text,
        structured: {
          ...structured,
          truncated: true,
          tokenEstimate,
          path,
          contentLength: wrappedDiff.length,
          writtenToFile: true,
        },
      }
    } catch (error) {
      const saveError = error instanceof Error ? error.message : String(error)
      const excerpt = diffText.slice(0, MAX_SAVE_FAILURE_EXCERPT_CHARS)
      const text = diff.urlChanged
        ? `URL changed; full current snapshot is ${tokenEstimate} estimated tokens, over the ${MAX_INLINE_DIFF_TOKENS}-token inline limit, but saving it to a BrowserOS output file failed: ${saveError}`
        : `Diff is ${tokenEstimate} estimated tokens, over the ${MAX_INLINE_DIFF_TOKENS}-token inline limit, but saving it to a BrowserOS output file failed: ${saveError}`
      return {
        text: [
          text,
          `Showing the first ${excerpt.length} chars instead:`,
          wrapUntrusted(excerpt, origin),
        ].join('\n'),
        structured: {
          ...structured,
          truncated: true,
          tokenEstimate,
          contentLength: wrappedDiff.length,
          writtenToFile: false,
          outputWriteFailed: true,
          error: saveError,
        },
      }
    }
  }

  if (diff.urlChanged) {
    return {
      text: `URL changed; returning full current snapshot instead of a diff:\n${wrappedDiff}`,
      structured,
    }
  }

  return {
    text: wrappedDiff,
    structured,
  }
}
