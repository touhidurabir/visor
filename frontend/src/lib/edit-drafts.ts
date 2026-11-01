// Unsaved edits of config files, kept in memory while the app is open, so
// switching tabs or screens does not lose them. A draft belongs to the
// content it was started from (its hash): when the file has since changed,
// the draft is stale and dropped.

interface Draft {
  hash: string
  text: string
}

const drafts = new Map<string, Draft>()

/** keepDraft remembers text as the edit of path, or forgets it when it equals the saved content. */
export function keepDraft(path: string, hash: string, saved: string, text: string) {
  if (text === saved) drafts.delete(path)
  else drafts.set(path, { hash, text })
}

/** takeDraft returns the kept edit of path if it started from content with this hash. */
export function takeDraft(path: string, hash: string): string | null {
  const d = drafts.get(path)
  if (!d) return null
  if (d.hash !== hash) {
    drafts.delete(path)
    return null
  }
  return d.text
}

export function dropDraft(path: string) {
  drafts.delete(path)
}
