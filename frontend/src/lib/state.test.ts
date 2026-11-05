import { describe, expect, it } from 'vitest'
import { stateLabel, stateLook, stateTextClass } from './state'

describe('stateLabel', () => {
  it.each([
    ['RUNNING', 'Running'],
    ['FATAL', 'Fatal'],
    ['BACKOFF', 'Backoff'],
    ['STOPPED', 'Stopped'],
    ['EXITED', 'Exited'],
    ['STARTING', 'Starting'],
    ['STOPPING', 'Stopping'],
    ['UNKNOWN', 'Unknown'],
    ['', ''],
  ])('%s → %s', (state, want) => {
    expect(stateLabel(state)).toBe(want)
  })
})

describe('stateLook', () => {
  it.each([
    // state, Go status, tone, alert
    ['RUNNING', 'running', 'ok', false],
    ['FATAL', 'failing', 'bad', true],
    ['BACKOFF', 'failing', 'warn', true],
    ['EXITED', 'failing', 'bad', true], // exited with a status it shouldn't have
    ['EXITED', 'stopped', 'idle', false], // exited as expected
    ['STOPPED', 'stopped', 'idle', false],
    ['STARTING', 'other', 'warn', false],
    ['STOPPING', 'other', 'warn', false],
    ['UNKNOWN', 'other', 'idle', false],
  ])('%s (%s) → %s, alert %s', (state, status, tone, alert) => {
    expect(stateLook(state, status)).toEqual({ label: stateLabel(state), tone, alert })
  })

  it('falls back to the state name without a status (history rows)', () => {
    expect(stateLook('RUNNING')).toMatchObject({ tone: 'ok', alert: false })
    expect(stateLook('FATAL')).toMatchObject({ tone: 'bad', alert: true })
    expect(stateLook('BACKOFF')).toMatchObject({ tone: 'warn', alert: true })
    expect(stateLook('EXITED')).toMatchObject({ tone: 'idle', alert: false })
    expect(stateLook('STOPPED')).toMatchObject({ tone: 'idle', alert: false })
  })
})

describe('stateTextClass', () => {
  it.each([
    // Running is green in regular weight; failing is coloured and semibold;
    // idle states are grey; Starting and Stopping are amber, regular.
    ['RUNNING', 'running', ['t-ok']],
    ['FATAL', 'failing', ['t-bad', 'alert']],
    ['BACKOFF', 'failing', ['t-warn', 'alert']],
    ['EXITED', 'failing', ['t-bad', 'alert']],
    ['EXITED', 'stopped', ['t-idle']],
    ['STOPPED', 'stopped', ['t-idle']],
    ['STARTING', 'other', ['t-warn']],
    ['UNKNOWN', 'other', ['t-idle']],
  ])('%s (%s) → %j', (state, status, want) => {
    expect(stateTextClass(stateLook(state, status))).toEqual(want)
  })
})
