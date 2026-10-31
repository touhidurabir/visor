import { describe, expect, it } from 'vitest'
import { diffLines, diffStats, isUnchanged, splitLines, withContext } from './diff'

const kinds = (a: string, b: string) => diffLines(a, b).map((l) => l.kind + l.text)

describe('diffLines', () => {
  it('equal texts have no change', () => {
    const d = diffLines('a\nb\n', 'a\nb\n')
    expect(d).toEqual([
      { kind: ' ', text: 'a' },
      { kind: ' ', text: 'b' },
    ])
    expect(isUnchanged(d)).toBe(true)
    expect(diffStats(d)).toEqual({ added: 0, removed: 0 })
  })

  it('shows an inserted line', () => {
    expect(kinds('a\nc', 'a\nb\nc')).toEqual([' a', '+b', ' c'])
  })

  it('shows a deleted line', () => {
    expect(kinds('a\nb\nc', 'a\nc')).toEqual([' a', '-b', ' c'])
  })

  it('shows a replaced line as removed then added', () => {
    const d = diffLines('a\ncommand=x\nc', 'a\ncommand=x --batch=50\nc')
    expect(d.map((l) => l.kind + l.text)).toEqual([' a', '-command=x', '+command=x --batch=50', ' c'])
    expect(diffStats(d)).toEqual({ added: 1, removed: 1 })
  })

  it('handles empty sides', () => {
    expect(kinds('', 'a\nb')).toEqual(['+a', '+b'])
    expect(kinds('a\nb', '')).toEqual(['-a', '-b'])
    expect(diffLines('', '')).toEqual([])
  })

  it('finds the common lines between scattered edits', () => {
    expect(kinds('1\n2\n3\n4\n5', '1\nx\n3\n4\ny')).toEqual([' 1', '-2', '+x', ' 3', ' 4', '-5', '+y'])
  })

  it('ignores a final newline but sees a blank line', () => {
    expect(isUnchanged(diffLines('a\n', 'a'))).toBe(true)
    expect(kinds('a', 'a\n\n')).toEqual([' a', '+'])
  })

  it('keeps duplicate lines apart', () => {
    expect(kinds('x\nx\nx', 'x\nx')).toEqual([' x', ' x', '-x'])
  })
})

describe('splitLines', () => {
  it('drops only the final empty line', () => {
    expect(splitLines('a\n\nb\n')).toEqual(['a', '', 'b'])
    expect(splitLines('')).toEqual([])
  })
})

describe('withContext', () => {
  const text = (n: number) => Array.from({ length: n }, (_, i) => `l${i + 1}`).join('\n')

  it('hides long unchanged runs', () => {
    const rows = withContext(diffLines(text(20), text(20).replace('l10', 'ten')), 2)
    expect(rows.map((r) => (r.kind === 'gap' ? `gap${r.hidden}` : r.kind + r.text))).toEqual([
      'gap7', ' l8', ' l9', '-l10', '+ten', ' l11', ' l12', 'gap8',
    ])
  })

  it('returns an unchanged diff whole', () => {
    expect(withContext(diffLines('a\nb', 'a\nb'))).toHaveLength(2)
  })
})
