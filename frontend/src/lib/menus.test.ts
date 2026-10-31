import { describe, expect, it } from 'vitest'
import { configMenu, processMenu, serverMenu, type MenuItem } from './menus'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const proc = (o: Partial<ProcessView>) =>
  ({ Name: 'web', Group: 'web', FullName: 'web', State: 'RUNNING', PID: 4242, ...o }) as ProcessView
const find = (items: MenuItem[], id: string) => items.find((i) => i.id === id)!

describe('processMenu', () => {
  it('lists the actions in order with supervisorctl commands', () => {
    const items = processMenu(proc({ FullName: 'g:web_00' }))
    expect(items.map((i) => i.label)).toEqual([
      'Start', 'Stop', 'Restart', 'Send signal…', 'View logs', 'Clear logs', 'Copy PID',
    ])
    expect(find(items, 'stop').cmd).toBe('supervisorctl stop g:web_00')
    expect(find(items, 'clear').cmd).toBe('supervisorctl clear g:web_00')
  })
  it('disables Start while running', () => {
    expect(find(processMenu(proc({ State: 'RUNNING' })), 'start').disabled).toBe(true)
    expect(find(processMenu(proc({ State: 'FATAL' })), 'start').disabled).toBe(false)
  })
  it.each(['STOPPED', 'FATAL', 'EXITED'])('disables Stop when %s', (State) => {
    expect(find(processMenu(proc({ State, PID: 0 })), 'stop').disabled).toBe(true)
  })
  it('disables Copy PID without a PID', () => {
    expect(find(processMenu(proc({ PID: 0 })), 'copy-pid').disabled).toBe(true)
    expect(find(processMenu(proc({ PID: 7 })), 'copy-pid').disabled).toBe(false)
  })
})

describe('configMenu', () => {
  const cfg = (Changed: boolean) => ({ FileName: 'mailer.conf', Changed }) as ConfigView
  it('enables Apply changes only when changed', () => {
    expect(find(configMenu(cfg(false)), 'apply').disabled).toBe(true)
    expect(find(configMenu(cfg(true)), 'apply').disabled).toBe(false)
  })
  it('confirms before removing from supervisor', () => {
    const rm = find(configMenu(cfg(false)), 'remove')
    expect(rm.danger).toBe(true)
    expect(rm.confirm).toBeTruthy()
  })
})

describe('serverMenu', () => {
  it('has 6 items; restart and shutdown confirm', () => {
    const items = serverMenu()
    expect(items).toHaveLength(6)
    expect(items.filter((i) => i.confirm).map((i) => i.id)).toEqual(['restart', 'shutdown'])
  })
})
