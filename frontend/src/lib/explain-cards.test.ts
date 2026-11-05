import { describe, expect, it } from 'vitest'
import type { ExplainLineView, ExplainSectionView, ExplainView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import {
  buildCards,
  cardCounts,
  cardOfLine,
  cardTitle,
  codeTokens,
  emptyOpen,
  hiddenNote,
  nextOpen,
  rollUp,
  sectionCounts,
  setAll,
  toggled,
} from './explain-cards'

const proc = (Program: string, State: string, Status: string, n = 0) => ({ Program, State, Status, FullName: `${Program}:${Program}_${n}` }) as ProcessView
const line = (Line: number, Level = '', Kind = 'key', Text = `k${Line}=v`): ExplainLineView => ({
  Line, Text, Key: `k${Line}`, Kind, Meaning: '', Level, Problems: null, Suggest: '', Add: null, Paths: null,
})
const section = (Name: string, lines: ExplainLineView[]): ExplainSectionView => ({
  Name, Program: Name.startsWith('program:') ? Name.slice(8) : '', Summary: '', Failure: '', Lines: [line(lines[0]?.Line ? lines[0].Line - 1 : 1, '', 'section', `[${Name}]`), ...lines],
})
const view = (sections: ExplainSectionView[], o: Partial<ExplainView> = {}): ExplainView => ({
  Summary: '', Errors: 0, SaveErrors: 0, Suggestions: 0, Settings: 0, Paths: 0, ParseError: null, FileProblems: null, Sections: sections, ...o,
})

describe('rollUp', () => {
  it('reads one process as its state', () => {
    expect(rollUp([proc('a', 'RUNNING', 'running')])).toEqual({ state: 'RUNNING', status: 'running', label: 'Running' })
    expect(rollUp([proc('a', 'STOPPED', 'stopped')]).label).toBe('Stopped')
  })
  it('reads copies all in one state as that state', () => {
    expect(rollUp([proc('a', 'RUNNING', 'running', 0), proc('a', 'RUNNING', 'running', 1)]).label).toBe('Running')
  })
  it('counts running copies in a mix', () => {
    const r = rollUp([proc('a', 'RUNNING', 'running', 0), proc('a', 'RUNNING', 'running', 1), proc('a', 'STOPPED', 'stopped', 2), proc('a', 'STOPPED', 'stopped', 3)])
    expect(r).toEqual({ state: 'RUNNING', status: 'running', label: '2 of 4 running' })
  })
  it('takes the colour of a copy that is starting', () => {
    const r = rollUp([proc('a', 'RUNNING', 'running', 0), proc('a', 'STARTING', 'other', 1)])
    expect(r).toEqual({ state: 'STARTING', status: 'other', label: '1 of 2 running' })
  })
  it('shows the worst failing state when any copy fails', () => {
    const r = rollUp([proc('a', 'RUNNING', 'running', 0), proc('a', 'EXITED', 'failing', 1), proc('a', 'FATAL', 'failing', 2), proc('a', 'BACKOFF', 'failing', 3)])
    expect(r).toEqual({ state: 'FATAL', status: 'failing', label: 'Fatal' })
    expect(rollUp([proc('a', 'RUNNING', 'running', 0), proc('a', 'BACKOFF', 'failing', 1)]).label).toBe('Backoff')
  })
  it('says a program without processes is not loaded', () => {
    expect(rollUp([])).toEqual({ state: '', status: '', label: 'Not loaded' })
  })
})

describe('cardTitle', () => {
  it('names a program by its name and other sections by kind and name', () => {
    expect(cardTitle(section('program:web', []))).toEqual({ kind: '', name: 'web' })
    expect(cardTitle(section('group:shop', []))).toEqual({ kind: 'Group', name: 'shop' })
    expect(cardTitle(section('eventlistener:mem', []))).toEqual({ kind: 'Event listener', name: 'mem' })
    expect(cardTitle(section('unix_http_server', []))).toEqual({ kind: '', name: '[unix_http_server]' })
  })
})

describe('buildCards', () => {
  const v = view([
    section('program:ok', [line(2), line(3)]),
    section('program:hint', [line(6), line(7, 'suggestion', 'unset')]),
    section('program:bad', [line(10, 'error'), line(11)]),
    section('group:g', [line(14)]),
  ])
  const procs = [proc('ok', 'RUNNING', 'running'), proc('hint', 'RUNNING', 'running'), proc('bad', 'FATAL', 'failing')]

  it('makes one card per section, with its live state and problem counts', () => {
    const { cards, hidden } = buildCards(v, procs, false)
    expect(hidden).toBe(0)
    expect(cards.map((c) => c.key)).toEqual(['program:ok', 'program:hint', 'program:bad', 'group:g'])
    expect(cards.map((c) => c.state?.label ?? null)).toEqual(['Running', 'Running', 'Fatal', null])
    expect(cards.map((c) => c.failing)).toEqual([false, false, true, false])
    expect(cards[1].suggestions).toBe(1)
    expect(cards[2].errors).toBe(1)
    expect(cards[0].lines).toHaveLength(3)
  })
  it('has no live state for a new file', () => {
    expect(buildCards(v, null, false).cards.every((c) => c.state === null)).toBe(true)
  })
  it('says a program supervisord has no process for is not loaded', () => {
    expect(buildCards(v, [], false).cards[0].state?.label).toBe('Not loaded')
  })
  it('fails a card on a failing live state even without an error line', () => {
    const c = buildCards(view([section('program:ok', [line(2)])]), [proc('ok', 'BACKOFF', 'failing')], false).cards[0]
    expect(c.failing).toBe(true)
  })
  it('keeps only problem lines and hides cards without problems under Only problems', () => {
    const { cards, hidden } = buildCards(v, procs, true)
    expect(cards.map((c) => c.key)).toEqual(['program:hint', 'program:bad'])
    expect(hidden).toBe(2)
    expect(cards[1].lines.map((l) => l.Line)).toEqual([10])
  })
  it('never hides a failing card, and shows all its lines when none is to blame', () => {
    const { cards } = buildCards(v, [proc('ok', 'FATAL', 'failing')], true)
    const ok = cards.find((c) => c.key === 'program:ok')!
    expect(ok.lines).toHaveLength(3)
  })
  it('gives repeated section names their own keys', () => {
    expect(buildCards(view([section('program:a', []), section('program:a', [])]), null, false).cards.map((c) => c.key)).toEqual(['program:a', 'program:a#2'])
  })
  it('makes no cards before loading or for a file it cannot read', () => {
    expect(buildCards(null, null, false).cards).toEqual([])
    expect(buildCards(view([], { ParseError: { Level: 'error', Live: false, Line: 1, Message: 'x', Fix: '', Edit: null } }), null, false).cards).toEqual([])
  })
})

describe('nextOpen', () => {
  const c = (key: string, failing = false) => ({ key, failing })

  it('opens failing cards and leaves the rest closed', () => {
    const s = nextOpen(emptyOpen(), [c('a'), c('b', true), c('c')])
    expect([...s.open]).toEqual(['b'])
  })
  it('opens the only card', () => {
    expect([...nextOpen(emptyOpen(), [c('a')]).open]).toEqual(['a'])
  })
  it('keeps what the user chose when the cards update', () => {
    let s = nextOpen(emptyOpen(), [c('a'), c('b', true)])
    s = toggled(s, 'a')
    s = toggled(s, 'b')
    s = nextOpen(s, [c('a'), c('b', true)])
    expect([...s.open]).toEqual(['a'])
  })
  it('opens a card when it starts failing, and not again while it stays failing', () => {
    let s = nextOpen(emptyOpen(), [c('a'), c('b')])
    s = nextOpen(s, [c('a', true), c('b')])
    expect([...s.open]).toEqual(['a'])
    s = toggled(s, 'a', false)
    s = nextOpen(s, [c('a', true), c('b')])
    expect([...s.open]).toEqual([])
  })
  it('decides a new card when it appears, and forgets cards that are gone', () => {
    let s = nextOpen(emptyOpen(), [c('a')])
    s = nextOpen(s, [c('a'), c('b')])
    expect([...s.open]).toEqual(['a'])
    s = nextOpen(s, [c('b')])
    expect([...s.known.keys()]).toEqual(['b'])
    expect([...s.open]).toEqual([])
  })
  it('expands and collapses all', () => {
    let s = nextOpen(emptyOpen(), [c('a'), c('b'), c('c', true)])
    s = setAll(s, ['a', 'b', 'c'], true)
    expect([...s.open].sort()).toEqual(['a', 'b', 'c'])
    s = setAll(s, ['a', 'b', 'c'], false)
    expect([...s.open]).toEqual([])
  })
})

describe('cardOfLine', () => {
  it('finds the card that holds a line', () => {
    const { cards } = buildCards(view([section('program:a', [line(2)]), section('program:b', [line(5), line(6)])]), null, false)
    expect(cardOfLine(cards, 6)).toBe('program:b')
    expect(cardOfLine(cards, 99)).toBe('')
  })
})

describe('header text', () => {
  it('counts programs, groups and other sections', () => {
    expect(sectionCounts(view([section('program:a', []), section('program:b', [])]))).toBe('2 programs')
    expect(sectionCounts(view([section('program:a', []), section('group:g', [])]))).toBe('1 program · 1 group')
    expect(sectionCounts(view([section('group:g', []), section('inet_http_server', [])]))).toBe('No programs · 1 group · 1 other section')
  })
  it('says how many cards Only problems hides', () => {
    expect(hiddenNote(1)).toBe('1 section without problems is hidden.')
    expect(hiddenNote(3)).toBe('3 sections without problems are hidden.')
  })
  it('counts a card’s problems', () => {
    expect(cardCounts({ errors: 1, suggestions: 2 })).toEqual([{ text: '1 error', cls: 'b-bad' }, { text: '2 suggestions', cls: 'b-warn' }])
    expect(cardCounts({ errors: 0, suggestions: 0 })).toEqual([])
  })
})

describe('codeTokens', () => {
  const join = (t: ReturnType<typeof codeTokens>) => t.map((x) => x.text).join('')

  it('splits a section header into brackets, kind and name', () => {
    const t = codeTokens('[program:cpcsl-reverb]', 'section')
    expect(t).toEqual([
      { text: '[', part: 'punct' },
      { text: 'program:', part: 'kind' },
      { text: 'cpcsl-reverb', part: 'name' },
      { text: ']', part: 'punct' },
    ])
  })
  it('splits a setting into key, equals and value', () => {
    expect(codeTokens('process_name=%(program_name)s', 'key')).toEqual([
      { text: 'process_name', part: 'key' },
      { text: '=', part: 'punct' },
      { text: '%(program_name)s', part: 'value' },
    ])
    expect(codeTokens('command = php artisan x', 'key').map((x) => x.part)).toEqual(['key', 'punct', 'value'])
  })
  it('keeps an inline comment apart', () => {
    const t = codeTokens('numprocs=2 ; two is enough', 'key')
    expect(t.map((x) => x.part)).toEqual(['key', 'punct', 'value', 'comment'])
    expect(join(t)).toBe('numprocs=2 ; two is enough')
    expect(codeTokens('[group:g] ; shop', 'section').at(-1)).toEqual({ text: ' ; shop', part: 'comment' })
  })
  it('keeps a value with ; inside it whole', () => {
    expect(codeTokens('command=sh -c "a;b"', 'key').map((x) => x.part)).toEqual(['key', 'punct', 'value'])
  })
  it('leaves other rows plain, and joins back to the line as written', () => {
    expect(codeTokens('not set: autostart', 'unset')).toEqual([{ text: 'not set: autostart', part: 'plain' }])
    for (const [text, kind] of [['[unix_http_server]', 'section'], ['  user = www', 'key'], ['empty=', 'key']])
      expect(join(codeTokens(text, kind))).toBe(text)
  })
})
