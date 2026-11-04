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
  const made = { fileName: 'mailer-copy.ini', content: copy }
  const now = (content: string, fileName = 'mailer-copy.ini') => ({ fileName, content })
  it('keeps every doubt while the copy is as Go made it', () => {
    expect(liveDoubts(doubts, made, made)).toEqual(doubts)
  })
  it('drops a doubt once its value no longer names the original', () => {
    const edited = copy.replace('/var/log/mailer.log', '/var/log/mailer-copy.log').replace('NAME="mailer"', 'NAME="other"')
    expect(liveDoubts(doubts, now(edited), made).map((d) => d.Key)).toEqual(['command', 'programs', ''])
  })
  it('drops a doubt whose line moved or lost its setting', () => {
    expect(liveDoubts(doubts, now(`; a new first line\n${copy}`), made)).toEqual([])
    expect(liveDoubts([doubt(40, 'command', 'mailer')], made, made)).toEqual([])
  })
  it('does not count %(program_name)s or a longer name as the original', () => {
    const text = '[program:mailer-copy]\ncommand=/bin/x --name mailer-copy\nuser=%(program_name)s\n'
    expect(liveDoubts([doubt(2, 'command', 'mailer'), doubt(3, 'user', 'mailer')], now(text), now('[program:mailer-copy]\ncommand=/bin/x --name mailer\nuser=mailer\n'))).toEqual([])
  })
  it('keeps a shared log file until its path changes, even when it doesn’t name the original', () => {
    const shared = '[program:worker-copy]\nstdout_logfile=/var/log/app.log ; both\nstderr_logfile=/var/log/app.err\n'
    const logs = [doubt(2, 'stdout_logfile', 'worker'), doubt(3, 'stderr_logfile', 'worker')]
    const was = now(shared)
    expect(liveDoubts(logs, was, was)).toEqual(logs)
    // Only the comment changed: still the same file.
    expect(liveDoubts(logs, now(shared.replace('; both', '; shared')), was)).toEqual(logs)
    const fixed = shared.replace('/var/log/app.log', '/var/log/%(program_name)s.log').replace('/var/log/app.err', '/var/log/app-copy.err')
    expect(liveDoubts(logs, now(fixed), was)).toEqual([])
  })
  it('reads a value on past blank and comment lines, as supervisord does', () => {
    const text = '[program:mailer-copy]\nenvironment=A="1",\n\n  ; the name\n  NAME="mailer"\ncommand=x\n'
    expect(liveDoubts([doubt(2, 'environment', 'mailer')], now(text), now(text))).toHaveLength(1)
    // A line that isn't indented ends the value.
    const ended = '[program:mailer-copy]\nenvironment=A="1",\n\nNAME="mailer"\n'
    expect(liveDoubts([doubt(2, 'environment', 'mailer')], now(ended), now(ended))).toEqual([])
  })
  it('keeps a doubt about the file name until the file is renamed', () => {
    const file = { Line: 0, Key: '', Name: 'web-copy.ini', Section: '', Message: 'web-copy.ini does not match the include setting' }
    const was = now('[program:web-copy]\n', 'web-copy.ini')
    expect(liveDoubts([file], was, was)).toEqual([file])
    expect(liveDoubts([file], now(was.content, 'web-copy-svc.ini'), was)).toEqual([])
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
