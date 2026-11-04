import { describe, expect, it } from 'vitest'
import type { CopyDoubt } from '../../bindings/github.com/touhidurabir/visor/internal/config'
import { doubtMarks, liveDoubts, mergeMarks, renamesNote } from './copy-draft'

const copy = `[program:mailer-copy]
command=/usr/bin/mail --queue mailer
stdout_logfile=/var/log/mailer.log
environment=A="1",
  NAME="mailer"
redirect_stderr=true

[group:jobs-copy]
programs=mailer-copy,shared

[eventlistener:watch]
command=/bin/watch
`
const doubt = (Line: number, Key: string, Name: string, Section = 'program:mailer-copy'): CopyDoubt => ({ Line, Key, Name, Section, Message: `${Key} still names ${Name}` })
const doubts = [
  doubt(2, 'command', 'mailer'),
  doubt(3, 'stdout_logfile', 'mailer'),
  doubt(4, 'environment', 'mailer'),
  doubt(9, 'programs', 'shared', 'group:jobs-copy'),
  doubt(11, '', 'watch', 'eventlistener:watch'),
]

describe('renamesNote', () => {
  it('lists each rename, saying which are groups', () => {
    expect(
      renamesNote([
        { Kind: 'program', From: 'mailer-worker', To: 'mailer-worker-copy' },
        { Kind: 'program', From: 'mailer-digest', To: 'mailer-digest-copy' },
        { Kind: 'group', From: 'mailer', To: 'mailer-copy' },
      ]),
    ).toBe('mailer-worker → mailer-worker-copy, mailer-digest → mailer-digest-copy, group mailer → mailer-copy')
    expect(renamesNote([])).toBe('')
  })
})

describe('liveDoubts', () => {
  it('keeps every doubt while the copy is as Go made it', () => {
    expect(liveDoubts(doubts, copy)).toEqual(doubts)
  })
  it('drops a doubt once its value no longer names the original', () => {
    const edited = copy.replace('/var/log/mailer.log', '/var/log/mailer-copy.log').replace('NAME="mailer"', 'NAME="other"')
    expect(liveDoubts(doubts, edited).map((d) => d.Key)).toEqual(['command', 'programs', ''])
  })
  it('drops a doubt whose line moved or lost its setting', () => {
    expect(liveDoubts(doubts, `; a new first line\n${copy}`)).toEqual([])
    expect(liveDoubts([doubt(40, 'command', 'mailer')], copy)).toEqual([])
  })
  it('does not count %(program_name)s or a longer name as the original', () => {
    const text = '[program:mailer-copy]\nstdout_logfile=/var/log/%(program_name)s.log\nstderr_logfile=/var/log/mailer-copy.err\n'
    expect(liveDoubts([doubt(2, 'stdout_logfile', 'mailer'), doubt(3, 'stderr_logfile', 'mailer')], text)).toEqual([])
  })
})

describe('editor marks', () => {
  it('marks each doubt line as a warning, once', () => {
    expect(doubtMarks([doubt(3, 'a', 'x'), doubt(3, 'b', 'x'), doubt(9, 'c', 'x')])).toEqual([
      { line: 3, level: 'warning' },
      { line: 9, level: 'warning' },
    ])
  })
  it('keeps the worst mark per line', () => {
    expect(mergeMarks([{ line: 3, level: 'error' }, { line: 1, level: 'warning' }], [{ line: 3, level: 'warning' }, { line: 5, level: 'warning' }])).toEqual([
      { line: 1, level: 'warning' },
      { line: 3, level: 'error' },
      { line: 5, level: 'warning' },
    ])
    expect(mergeMarks([{ line: 2, level: 'warning' }], [{ line: 2, level: 'error' }])).toEqual([{ line: 2, level: 'error' }])
  })
})
