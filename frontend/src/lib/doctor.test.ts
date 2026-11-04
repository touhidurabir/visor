import { describe, expect, it } from 'vitest'
import type { Status, Step } from '../../bindings/github.com/touhidurabir/visor/internal/doctor'
import { chownCommand, lensText, mkdirCommand, rootFixes, situationCopy } from './doctor'

const step = (s: string): Step => ({ ID: '', Title: '', Detail: '', Status: s as Status })

describe('situationCopy', () => {
  it('has the board’s words for each situation', () => {
    expect(situationCopy('ready').headline).toBe('Your Mac is ready')
    expect(situationCopy('noconf').headline).toBe('Where is your config?')
    expect(situationCopy('nosup').headline).toBe('supervisord isn’t installed yet')
    expect(situationCopy('down').headline).toBe('supervisord isn’t running')
    expect(situationCopy('denied').headline).toBe('supervisord won’t let Visor in')
    expect(situationCopy('notwritable').headline).toBe('Visor can watch, but not edit')
    expect(situationCopy('auth').headline).toBe('supervisord wants a password')
  })
  it('reads an unknown situation as ready', () => {
    expect(situationCopy('???')).toEqual(situationCopy('ready'))
  })
})

describe('lensText', () => {
  it('counts what passed, or what to fix', () => {
    expect(lensText({ Steps: ['ok', 'ok', 'ok', 'ok', 'ok'].map(step) })).toEqual({ big: '✓', small: '5 of 5 checks passed' })
    expect(lensText({ Steps: ['ok', 'ok', 'bad', 'ok', 'wait'].map(step) })).toEqual({ big: '!', small: '1 thing to fix' })
    expect(lensText({ Steps: ['ok', 'ok', 'bad', 'bad', 'wait'].map(step) })).toEqual({ big: '!', small: '2 things to fix' })
  })
})

describe('commands', () => {
  it('quotes a folder with spaces', () => {
    expect(chownCommand('/opt/homebrew/etc/supervisor.d')).toBe('sudo chown -R $(whoami) /opt/homebrew/etc/supervisor.d')
    expect(chownCommand('/Users/me/My Configs')).toBe("sudo chown -R $(whoami) '/Users/me/My Configs'")
    expect(chownCommand('')).toBe('')
    expect(mkdirCommand('/Users/me/My Configs')).toBe("mkdir -p '/Users/me/My Configs'")
  })
  it('names the user in the socket fix', () => {
    const fixes = rootFixes('abir')
    expect(fixes).toHaveLength(3)
    expect(fixes[1].commands[0]).toBe('chown=abir:staff')
    expect(rootFixes('')[1].commands[0]).toBe('chown=you:staff')
  })
})
