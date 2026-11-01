import { describe, expect, it } from 'vitest'
import { bulkConfirm, comingSoon, configMenu, ctlName, processMenu, serverMenu, type MenuItem } from './menus'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const proc = (o: Partial<ProcessView>) =>
  ({ Name: 'web', Group: 'web', FullName: 'web', State: 'RUNNING', PID: 4242, ...o }) as ProcessView
const find = (items: MenuItem[], id: string) => items.find((i) => i.id === id)!

describe('ctlName', () => {
  it('uses the bare name for a lone process whose group has the same name', () => {
    expect(ctlName(proc({ Name: 'mailer-digest', Group: 'mailer-digest', FullName: 'mailer-digest:mailer-digest' }), 1)).toBe('mailer-digest')
  })
  it('keeps group:name for a numbered process or a bigger group', () => {
    expect(ctlName(proc({ Name: 'shop-queue_00', Group: 'shop-queue', FullName: 'shop-queue:shop-queue_00' }), 4)).toBe('shop-queue:shop-queue_00')
    expect(ctlName(proc({ Name: 'web', Group: 'web', FullName: 'web:web' }), 2)).toBe('web:web')
  })
})

describe('processMenu', () => {
  it('lists the actions in order with supervisorctl commands', () => {
    const items = processMenu(proc({ Name: 'web_00', Group: 'g', FullName: 'g:web_00' }), 1)
    expect(items.map((i) => i.label)).toEqual([
      'Start', 'Stop', 'Restart', 'Send signal…', 'View logs', 'Clear logs', 'Copy PID',
    ])
    expect(find(items, 'stop').cmd).toBe('supervisorctl stop g:web_00')
    expect(find(items, 'clear').cmd).toBe('supervisorctl clear g:web_00')
  })
  it('disables Start while running', () => {
    expect(find(processMenu(proc({ State: 'RUNNING' }), 1), 'start').disabled).toBe(true)
    expect(find(processMenu(proc({ State: 'FATAL' }), 1), 'start').disabled).toBe(false)
  })
  it.each(['STOPPED', 'FATAL', 'EXITED'])('disables Stop when %s', (State) => {
    expect(find(processMenu(proc({ State, PID: 0 }), 1), 'stop').disabled).toBe(true)
  })
  it('confirms before clearing the logs, like the Logs tab', () => {
    const clear = find(processMenu(proc({ Name: 'web_00', Group: 'g', FullName: 'g:web_00' }), 1), 'clear')
    expect(clear.danger).toBe(true)
    expect(clear.confirm).toBe('supervisord empties both the stdout and the stderr log files of g:web_00. This can’t be undone.')
    expect(clear.disabled).toBe(false)
  })
  it.each(['STOPPED', 'FATAL', 'EXITED', 'STARTING', 'BACKOFF'])('disables Clear logs when %s, saying why', (State) => {
    const clear = find(processMenu(proc({ State }), 1), 'clear')
    expect(clear.disabled).toBe(true)
    expect(clear.title).toBe('supervisord can clear the logs of a running process only')
  })
  it('disables Copy PID without a PID', () => {
    expect(find(processMenu(proc({ PID: 0 }), 1), 'copy-pid').disabled).toBe(true)
    expect(find(processMenu(proc({ PID: 7 }), 1), 'copy-pid').disabled).toBe(false)
  })
})

describe('configMenu', () => {
  const cfg = (Changed: boolean) => ({ FileName: 'mailer.conf', Changed }) as ConfigView
  it('enables Apply changes only when changed', () => {
    expect(find(configMenu(cfg(false)), 'apply').disabled).toBe(true)
    expect(find(configMenu(cfg(true)), 'apply').disabled).toBe(false)
  })
  it('derives group targets from the processes, not the file name', () => {
    const c = {
      FileName: 'mailer.conf',
      Changed: true,
      Processes: [proc({ Group: 'mail' }), proc({ Group: 'mail' }), proc({ Group: 'digest' })],
    } as ConfigView
    const items = configMenu(c)
    expect(find(items, 'start-all').cmd).toBe('supervisorctl start mail:* digest:*')
    expect(find(items, 'apply').cmd).toBe('supervisorctl update mail digest')
    expect(find(items, 'remove').cmd).toBe('supervisorctl remove mail digest')
  })
  it('shows <group> when the file has no processes', () => {
    expect(find(configMenu(cfg(false)), 'stop-all').cmd).toBe('supervisorctl stop <group>:*')
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

describe('comingSoon', () => {
  it('disables only the named items and explains why', () => {
    const items = comingSoon(serverMenu(), ['apply-all'])
    expect(find(items, 'apply-all')).toMatchObject({ disabled: true, title: 'Coming in this release' })
    expect(find(items, 'check').disabled).toBe(false)
  })
})

describe('bulkConfirm', () => {
  it('starting everything needs no confirmation', () => {
    expect(bulkConfirm('start', 12)).toBeNull()
  })
  it('stopping everything asks first and names the count', () => {
    const c = bulkConfirm('stop', 12)!
    expect(c.title).toBe('Stop all processes?')
    expect(c.message).toContain('12 processes')
    expect(c.confirmLabel).toBe('Stop all')
  })
  it('names the file when the action covers one config', () => {
    const c = bulkConfirm('stop', 3, 'mailer.ini')!
    expect(c.title).toBe('Stop all processes in mailer.ini?')
    expect(c.message).toContain('all 3 processes in mailer.ini')
    expect(bulkConfirm('start', 3, 'mailer.ini')).toBeNull()
  })
  it('restarting everything asks first', () => {
    const c = bulkConfirm('restart', 1)!
    expect(c.message).toContain('1 process ')
    expect(c.confirmLabel).toBe('Restart all')
  })
})
