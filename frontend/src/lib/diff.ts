// A line diff (longest common subsequence) for the config editor: the save
// preview, "changed on disk vs. what supervisord runs", and backups. Pure,
// so it runs under vitest.

export type DiffKind = ' ' | '-' | '+'

export interface DiffLine {
  kind: DiffKind
  text: string
}

/** Splits text into lines; a final newline does not add an empty line. */
export function splitLines(text: string): string[] {
  if (text === '') return []
  const lines = text.split('\n')
  if (lines[lines.length - 1] === '') lines.pop()
  return lines
}

// Beyond this many table cells the LCS table gets too big; the middle part
// is then shown as all removed and all added, which is still correct.
const MAX_CELLS = 4_000_000

/**
 * diffLines compares two texts line by line. Unchanged lines have kind ' ',
 * lines only in `from` '-', lines only in `to` '+'. In a replaced block the
 * removed lines come before the added ones.
 */
export function diffLines(from: string, to: string): DiffLine[] {
  const a = splitLines(from)
  const b = splitLines(to)
  let start = 0
  while (start < a.length && start < b.length && a[start] === b[start]) start++
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--
    endB--
  }
  const out: DiffLine[] = a.slice(0, start).map((text) => ({ kind: ' ', text }))
  out.push(...middle(a.slice(start, endA), b.slice(start, endB)))
  out.push(...a.slice(endA).map((text): DiffLine => ({ kind: ' ', text })))
  return out
}

function middle(a: string[], b: string[]): DiffLine[] {
  if (a.length === 0) return b.map((text) => ({ kind: '+', text }))
  if (b.length === 0) return a.map((text) => ({ kind: '-', text }))
  if ((a.length + 1) * (b.length + 1) > MAX_CELLS) {
    return [...a.map((text): DiffLine => ({ kind: '-', text })), ...b.map((text): DiffLine => ({ kind: '+', text }))]
  }
  // lcs[i][j] = length of the LCS of a[i:] and b[j:].
  const w = b.length + 1
  const lcs = new Uint32Array((a.length + 1) * w)
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i * w + j] = a[i] === b[j] ? lcs[(i + 1) * w + j + 1] + 1 : Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1])
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ kind: ' ', text: a[i] })
      i++
      j++
    } else if (lcs[(i + 1) * w + j] >= lcs[i * w + j + 1]) {
      out.push({ kind: '-', text: a[i++] })
    } else {
      out.push({ kind: '+', text: b[j++] })
    }
  }
  while (i < a.length) out.push({ kind: '-', text: a[i++] })
  while (j < b.length) out.push({ kind: '+', text: b[j++] })
  return out
}

/** How many lines were added and removed. */
export function diffStats(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const l of lines) {
    if (l.kind === '+') added++
    else if (l.kind === '-') removed++
  }
  return { added, removed }
}

/** True when the diff has no added or removed line. */
export function isUnchanged(lines: DiffLine[]): boolean {
  return lines.every((l) => l.kind === ' ')
}

export type DiffRow = DiffLine | { kind: 'gap'; hidden: number }

/**
 * withContext keeps `context` unchanged lines around each change and
 * replaces longer runs of unchanged lines with one gap row. With no change
 * at all it returns the diff as is.
 */
export function withContext(lines: DiffLine[], context = 3): DiffRow[] {
  if (isUnchanged(lines)) return lines
  const keep = new Array<boolean>(lines.length).fill(false)
  lines.forEach((l, i) => {
    if (l.kind === ' ') return
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep[k] = true
  })
  const out: DiffRow[] = []
  let hidden = 0
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (hidden > 0) out.push({ kind: 'gap', hidden })
      hidden = 0
      out.push(l)
    } else {
      hidden++
    }
  })
  if (hidden > 0) out.push({ kind: 'gap', hidden })
  return out
}
