// "What this config does" as one card per section (Mac feedback 1): which
// lines each card shows, its live state rolled up from its processes, which
// cards start open, the header counts, and the config line split into
// highlighted parts. Everything comes from what Go already explained
// (ExplainView) and the live overview (ProcessView); nothing here polls.
import type { ExplainLineView, ExplainSectionView, ExplainView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { isProblem } from './explain'
import { stateLabel } from './state'

/** A program's live state on its card: what StateText shows. */
export interface CardState {
  /** The supervisord state the dot and colour follow ("" when not loaded). */
  state: string
  /** ProcessView.Status of that state ("failing", "running", ...). */
  status: string
  /** The words: "Running", "Fatal", "2 of 4 running", "Not loaded". */
  label: string
}

// Worst first: what a card says when several of its copies are failing.
const failRank = (state: string) => ['FATAL', 'BACKOFF', 'EXITED'].indexOf(state) >>> 0

/**
 * rollUp is the state of a program from its processes (its copies when
 * numprocs > 1): a failing copy wins with the worst failing state; copies
 * all in one state read as that state; a mix reads "2 of 4 running". No
 * process means supervisord hasn't loaded the program.
 */
export function rollUp(procs: ProcessView[]): CardState {
  if (!procs.length) return { state: '', status: '', label: 'Not loaded' }
  const failing = procs.filter((p) => p.Status === 'failing').sort((a, b) => failRank(a.State) - failRank(b.State))
  if (failing.length) return { state: failing[0].State, status: 'failing', label: stateLabel(failing[0].State) }
  const first = procs[0]
  if (procs.every((p) => p.State === first.State)) return { state: first.State, status: first.Status, label: stateLabel(first.State) }
  // A mix: a copy on its way up or down sets the colour, else running does.
  const moving = procs.find((p) => p.State === 'STARTING' || p.State === 'STOPPING')
  const running = procs.filter((p) => p.State === 'RUNNING').length
  const lead = moving ?? procs.find((p) => p.State === 'RUNNING') ?? first
  const label = running ? `${running} of ${procs.length} running` : stateLabel(lead.State)
  return { state: lead.State, status: lead.Status, label }
}

/**
 * copiesLine is the roll-up of several copies of one program in words
 * that count: "2 of 2 running", or "3 of 4 running · 1 failing". It counts
 * running and failing the way rollUp does (RUNNING, and Go's "failing"
 * status), so a popover row and the program's card agree.
 */
export function copiesLine(procs: Pick<ProcessView, 'State' | 'Status'>[]): string {
  const running = procs.filter((p) => p.State === 'RUNNING').length
  const failing = procs.filter((p) => p.Status === 'failing').length
  const line = `${running} of ${procs.length} running`
  return failing ? `${line} · ${failing} failing` : line
}

/** The card's title: a program by its name, any other section by its kind and name ("Group", "shop"). */
export function cardTitle(s: ExplainSectionView): { kind: string; name: string } {
  if (s.Program) return { kind: '', name: s.Program }
  const [kind, ...rest] = s.Name.split(':')
  if (!rest.length) return { kind: '', name: `[${s.Name}]` }
  const word = kind === 'eventlistener' ? 'Event listener' : kind === 'fcgi-program' ? 'FastCGI program' : kind.charAt(0).toUpperCase() + kind.slice(1)
  return { kind: word, name: rest.join(':') }
}

export interface Card {
  /** Stable while the section keeps its name: "program:web" (a repeat gets "#2"). */
  key: string
  section: ExplainSectionView
  title: { kind: string; name: string }
  /** Live state; null for a section that isn't a program, or when there is no live state (Add new). */
  state: CardState | null
  /** Lines with an error, and lines with a warning or a suggestion. */
  errors: number
  suggestions: number
  /** Failing now (a failing live state) or in the text (an error on a line): opens the card. */
  failing: boolean
  /** The lines to show, after "Only problems". */
  lines: ExplainLineView[]
}

/**
 * buildCards turns an explanation into cards. procs are the processes
 * supervisord runs from this file, or null when there is no live state
 * (a new file). With onlyProblems, each card shows only its problem lines
 * and a card with none is left out (hidden counts them); a failing card is
 * never left out, and shows every line when no single line is to blame.
 */
export function buildCards(view: ExplainView | null, procs: ProcessView[] | null, onlyProblems: boolean): { cards: Card[]; hidden: number } {
  const cards: Card[] = []
  let hidden = 0
  if (!view || view.ParseError) return { cards, hidden }
  const seen = new Map<string, number>()
  for (const s of view.Sections ?? []) {
    const n = (seen.get(s.Name) ?? 0) + 1
    seen.set(s.Name, n)
    const all = s.Lines ?? []
    const state = s.Program && procs ? rollUp(procs.filter((p) => p.Program === s.Program)) : null
    const errors = all.filter((l) => l.Level === 'error').length
    const suggestions = all.filter((l) => l.Level === 'warning' || l.Level === 'suggestion').length
    const failing = errors > 0 || state?.status === 'failing'
    let lines = all
    if (onlyProblems) {
      const problems = all.filter(isProblem)
      if (!problems.length && !failing) {
        hidden++
        continue
      }
      lines = problems.length ? problems : all
    }
    cards.push({ key: n > 1 ? `${s.Name}#${n}` : s.Name, section: s, title: cardTitle(s), state, errors, suggestions, failing, lines })
  }
  return { cards, hidden }
}

/** Which cards are open, and what Visor last saw of each card (failing or not). */
export interface OpenState {
  open: Set<string>
  known: Map<string, boolean>
}

export const emptyOpen = (): OpenState => ({ open: new Set(), known: new Map() })

/**
 * nextOpen decides which cards are open after the cards change. A card
 * seen for the first time opens when it is failing, or when it is the
 * file's only card; the rest start closed (a suggestion alone opens
 * nothing). A card that starts failing later opens then. Otherwise each
 * card keeps what the user chose. all is every card, not only the ones
 * "Only problems" shows, so filtering never re-decides anything.
 */
export function nextOpen(prev: OpenState, all: Pick<Card, 'key' | 'failing'>[]): OpenState {
  const open = new Set<string>()
  const known = new Map<string, boolean>()
  for (const c of all) {
    const was = prev.known.get(c.key)
    const isOpen = was === undefined ? c.failing || all.length === 1 : prev.open.has(c.key) || (c.failing && !was)
    if (isOpen) open.add(c.key)
    known.set(c.key, c.failing)
  }
  return { open, known }
}

/** Opens or closes one card. */
export function toggled(s: OpenState, key: string, to = !s.open.has(key)): OpenState {
  const open = new Set(s.open)
  if (to) open.add(key)
  else open.delete(key)
  return { open, known: s.known }
}

/** Opens (or closes) every card in keys. */
export function setAll(s: OpenState, keys: string[], to: boolean): OpenState {
  return { open: to ? new Set([...s.open, ...keys]) : new Set([...s.open].filter((k) => !keys.includes(k))), known: s.known }
}

/** The key of the card that holds line n (1-based), or "". */
export function cardOfLine(cards: Card[], n: number): string {
  return cards.find((c) => (c.section.Lines ?? []).some((l) => l.Kind !== 'unset' && l.Line === n))?.key ?? ''
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** "7 programs", "2 programs · 1 group", "No programs". */
export function sectionCounts(view: ExplainView | null): string {
  const secs = view?.Sections ?? []
  const programs = secs.filter((s) => s.Program).length
  const groups = secs.filter((s) => s.Name.startsWith('group:')).length
  const other = secs.length - programs - groups
  const parts = [programs ? plural(programs, 'program', 'programs') : 'No programs']
  if (groups) parts.push(plural(groups, 'group', 'groups'))
  if (other) parts.push(plural(other, 'other section', 'other sections'))
  return parts.join(' · ')
}

/** Under "Only problems": "2 programs without problems are hidden." */
export function hiddenNote(n: number): string {
  return n === 1 ? '1 section without problems is hidden.' : `${n} sections without problems are hidden.`
}

/** A card's problem count for its header: "1 error", "2 suggestions". */
export function cardCounts(c: Pick<Card, 'errors' | 'suggestions'>): { text: string; cls: string }[] {
  const out: { text: string; cls: string }[] = []
  if (c.errors) out.push({ text: plural(c.errors, 'error', 'errors'), cls: 'b-bad' })
  if (c.suggestions) out.push({ text: plural(c.suggestions, 'suggestion', 'suggestions'), cls: 'b-warn' })
  return out
}

/** One highlighted part of a config line. */
export interface CodeToken {
  text: string
  /** punct: [ ] = ; kind: "program:"; name: the section's name; key; value; comment; plain. */
  part: 'punct' | 'kind' | 'name' | 'key' | 'value' | 'comment' | 'plain'
}

// supervisord's inline comment: a ";" after whitespace.
function withComment(text: string, part: 'value' | 'plain'): CodeToken[] {
  const m = /\s;/.exec(text)
  if (!m) return text ? [{ text, part }] : []
  const before = text.slice(0, m.index)
  return [...(before ? [{ text: before, part }] : []), { text: text.slice(m.index), part: 'comment' as const }]
}

/**
 * codeTokens splits a config line for highlighting: a section header into
 * its brackets, kind and name ("[", "program:", "web", "]"); a setting
 * into its key, the "=" and its value; an inline comment apart. Joined,
 * the parts are the line as written.
 */
export function codeTokens(text: string, kind: string): CodeToken[] {
  if (kind === 'section') {
    const m = /^(\s*\[)([^\]:]*:)?([^\]]*)(\])(.*)$/.exec(text)
    if (!m) return [{ text, part: 'plain' }]
    const out: CodeToken[] = [{ text: m[1], part: 'punct' }]
    if (m[2]) out.push({ text: m[2], part: 'kind' })
    if (m[3]) out.push({ text: m[3], part: 'name' })
    out.push({ text: m[4], part: 'punct' }, ...withComment(m[5], 'plain'))
    return out
  }
  if (kind === 'key') {
    const m = /^(\s*[^=:\s]+)(\s*[=:]\s*)(.*)$/.exec(text)
    if (!m) return [{ text, part: 'plain' }]
    return [{ text: m[1], part: 'key' }, { text: m[2], part: 'punct' }, ...withComment(m[3], 'value')]
  }
  return [{ text, part: 'plain' }]
}
