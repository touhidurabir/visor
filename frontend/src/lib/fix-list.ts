// "To fix before saving": the short list of problems at the top of Line by
// line, the reason next to a disabled Save, and the Form's "things to check".
// All from what Go already explained (ExplainView) or checked (Check).
import type { ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'
import { fixToSave } from './explain'

export interface FixItem {
  /** 1-based line; 0 when the problem is about the whole file. */
  line: number
  level: 'error' | 'warning'
  text: string
}

const rank = (l: FixItem['level']) => (l === 'error' ? 0 : 1)

/**
 * Every problem in the text, errors first, then by line. What supervisord
 * says about the running program (Live) is left out: saving can't fix it.
 */
export function fixItems(v: ExplainView | null): FixItem[] {
  if (!v) return []
  if (v.ParseError) return [{ line: v.ParseError.Line, level: 'error', text: v.ParseError.Message }]
  const out: FixItem[] = []
  const seen = new Set<string>()
  const put = (line: number, level: string, text: string) => {
    const lv: FixItem['level'] = level === 'error' ? 'error' : 'warning'
    const k = `${line}|${lv}|${text}`
    if (seen.has(k)) return
    seen.add(k)
    out.push({ line, level: lv, text })
  }
  for (const p of v.FileProblems ?? []) if (!p.Live) put(p.Line, p.Level, p.Message)
  for (const s of v.Sections ?? [])
    for (const l of s.Lines ?? []) for (const p of l.Problems ?? []) if (!p.Live) put(l.Line || p.Line, p.Level, p.Message)
  return out.sort((a, b) => rank(a.level) - rank(b.level) || a.line - b.line)
}

/** "3 and 15", "3, 7 and 15". */
function joinNumbers(ns: number[]): string {
  return ns.length === 1 ? String(ns[0]) : `${ns.slice(0, -1).join(', ')} and ${ns[ns.length - 1]}`
}

/**
 * Why Save is disabled, naming the lines: "Fix line 22 to save", "Fix lines
 * 3 and 15 to save"; a count past three lines, or "problems" when one is
 * about the whole file. "" when no error blocks saving.
 */
export function saveReason(v: ExplainView | null): string {
  const errors = fixItems(v).filter((i) => i.level === 'error')
  if (!errors.length) return v?.SaveErrors ? fixToSave(v.SaveErrors, false) : ''
  if (errors.some((i) => !i.line)) return fixToSave(errors.length, false)
  const lines = [...new Set(errors.map((i) => i.line))].sort((a, b) => a - b)
  if (lines.length > 3) return fixToSave(lines.length)
  return `Fix ${lines.length === 1 ? 'line' : 'lines'} ${joinNumbers(lines)} to save`
}

export interface CheckItem {
  /** The form field it belongs to ("" when the form has none for it). */
  key: string
  /** The field's label, or "Line N" / "The file". */
  label: string
  level: 'error' | 'warning'
  text: string
}

/**
 * The Form's "N things to check": the errors and warnings of the checks,
 * each under the field it is about (labelOf gives a field's label, or ""
 * when the form has no field for that key). A check about the whole program
 * belongs to the name field, as in fieldNotes. Errors come first.
 */
export function checkItems(checks: Check[], labelOf: (key: string) => string): CheckItem[] {
  const out: CheckItem[] = []
  for (const c of checks) {
    if (c.Level !== 'error' && c.Level !== 'warning') continue
    const key = c.Program || c.Key ? c.Key || 'name' : ''
    const label = key ? labelOf(key) : ''
    out.push({
      key: label ? key : '',
      label: label || (c.Line > 0 ? `Line ${c.Line}` : 'The file'),
      level: c.Level === 'error' ? 'error' : 'warning',
      text: c.Message,
    })
  }
  return out.sort((a, b) => rank(a.level) - rank(b.level))
}

/**
 * Why the Form can't save, by field: "Fix “Output log” to save", "Fix
 * “Copies” and “Copy name” to save", "Fix 3 settings to save". "" when
 * nothing blocks.
 */
export function formReason(items: CheckItem[]): string {
  const labels = [...new Set(items.filter((i) => i.level === 'error').map((i) => i.label))]
  if (!labels.length) return ''
  if (labels.length > 2) return `Fix ${labels.length} settings to save`
  return `Fix ${labels.map((l) => `“${l}”`).join(' and ')} to save`
}

/** "1 thing to check", "3 things to check". */
export function thingsToCheck(n: number): string {
  return `${n} ${n === 1 ? 'thing' : 'things'} to check`
}
