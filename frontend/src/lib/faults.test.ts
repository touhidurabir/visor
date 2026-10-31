import { describe, expect, it } from 'vitest'
import { describeError, faultMessage } from './faults'

describe('faultMessage', () => {
  it.each([
    ['BAD_NAME', /no process with that name/i],
    ['NOT_RUNNING', /isn.t running/i],
    ['ALREADY_STARTED', /already running/i],
    ['SPAWN_ERROR', /could not start/i],
    ['ABNORMAL_TERMINATION', /stopped right after starting/i],
    ['NO_FILE', /can.t find the command/i],
    ['NOT_EXECUTABLE', /isn.t executable/i],
    ['UNREACHABLE', /can.t reach supervisord/i],
  ])('maps %s to plain English', (code, re) => {
    expect(faultMessage(code)).toMatch(re)
  })
  it('falls back to the code for unknown faults', () => {
    expect(faultMessage('WEIRD')).toBe('supervisord refused the request (WEIRD).')
  })
})

describe('describeError', () => {
  it('extracts the code from a Go fault string', () => {
    expect(describeError(new Error('xmlrpc fault 60: ALREADY_STARTED'))).toBe(faultMessage('ALREADY_STARTED'))
    expect(describeError('start web: xmlrpc fault 70: NOT_RUNNING')).toBe(faultMessage('NOT_RUNNING'))
  })
  it('maps connection errors to unreachable', () => {
    expect(describeError(new Error('supervisord: unreachable: dial unix /x.sock: no such file'))).toBe(
      faultMessage('UNREACHABLE'),
    )
  })
  it('passes other messages through', () => {
    expect(describeError(new Error('boom'))).toBe('boom')
  })
})
