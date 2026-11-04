import { describe, expect, it } from 'vitest'
import type { ServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { connectionProblem, isDown } from './connection'

const server = (s: Partial<ServerView>) => ({ Reachable: false, Health: 'error', Error: '', ...s }) as ServerView
const line = (s: Partial<ServerView>) => connectionProblem(server(s))!.line.map((x) => x.text).join('')

const NOT_RUNNING =
  'cannot reach supervisord: supervisord: unreachable: Post "http://localhost/RPC2": dial unix /tmp/visor-e2e/s.sock: connect: no such file or directory'
const REFUSED = 'cannot reach supervisord: supervisord: unreachable: Post "http://127.0.0.1:9001/RPC2": dial tcp 127.0.0.1:9001: connect: connection refused'
const DENIED = 'cannot reach supervisord: supervisord: unreachable: Post "http://localhost/RPC2": dial unix /opt/homebrew/var/run/supervisor.sock: connect: permission denied'
const AUTH = 'cannot reach supervisord: supervisord: unauthorized'

describe('isDown', () => {
  it('is the sidebar’s test: unreachable once the first poll has said so', () => {
    expect(isDown(null)).toBe(false)
    expect(isDown(server({ Reachable: true, Health: 'ok' }))).toBe(false)
    expect(isDown(server({ Health: 'unknown' }))).toBe(false) // no poll yet
    expect(isDown(server({ Health: 'warning', Error: NOT_RUNNING }))).toBe(true) // the first failed poll
    expect(isDown(server({ Health: 'error', Error: NOT_RUNNING }))).toBe(true)
  })
})

describe('connectionProblem', () => {
  it('is null while connected or not yet known', () => {
    expect(connectionProblem(undefined)).toBeNull()
    expect(connectionProblem(server({ Reachable: true, Health: 'ok' }))).toBeNull()
    expect(connectionProblem(server({ Health: 'unknown' }))).toBeNull()
  })

  it('says supervisord isn’t running when nothing answers, naming the socket, and keeps the raw error', () => {
    const p = connectionProblem(server({ Error: NOT_RUNNING }))!
    expect(p).toMatchObject({ kind: 'down', title: 'supervisord isn’t running', address: '/tmp/visor-e2e/s.sock', retrying: true, raw: NOT_RUNNING })
    expect(p.reason).toBe('supervisord isn’t running')
    expect(p.line).toEqual([
      { text: 'Nothing answers at ' },
      { text: '/tmp/visor-e2e/s.sock', mono: true },
      { text: ', so Visor can’t show or control processes. Your config files are fine.' },
    ])
  })

  it('names a TCP address too, and copes with an error that names none', () => {
    expect(connectionProblem(server({ Error: REFUSED }))).toMatchObject({ kind: 'down', address: '127.0.0.1:9001' })
    const bare = 'cannot reach supervisord: supervisord: unreachable: context deadline exceeded'
    expect(connectionProblem(server({ Error: bare }))).toMatchObject({ kind: 'down', address: '' })
    expect(line({ Error: bare })).toBe('Nothing answers on its socket, so Visor can’t show or control processes. Your config files are fine.')
  })

  it('never says “isn’t running” for a socket it may not open or a turned-down password', () => {
    expect(connectionProblem(server({ Error: DENIED }))).toMatchObject({ kind: 'denied', title: 'supervisord won’t let Visor in', retrying: true })
    expect(connectionProblem(server({ Error: AUTH }))).toMatchObject({ kind: 'auth', title: 'supervisord wants a password', retrying: true })
    expect(line({ Error: AUTH })).toContain('turned down the username and password')
    for (const e of [DENIED, AUTH]) expect(connectionProblem(server({ Error: e }))!.title).not.toMatch(/isn’t running/)
  })

  it('reads a setup error (no polling) as such', () => {
    const pw = 'cannot read the supervisord password: access denied'
    expect(connectionProblem(server({ Error: pw }))).toMatchObject({ kind: 'password', title: 'Visor can’t read the saved password', retrying: false })
    const noSocket = '/etc/supervisord.conf defines no socket or port: add an [unix_http_server] section with file= set'
    expect(connectionProblem(server({ Error: noSocket }))).toMatchObject({ kind: 'setup', title: 'Visor can’t connect to supervisord', retrying: false, raw: noSocket })
    expect(connectionProblem(server({ Error: '' }))).toMatchObject({ kind: 'setup', raw: '' })
  })
})
