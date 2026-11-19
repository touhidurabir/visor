import { beforeEach, describe, expect, it } from 'vitest'
import type { RouteLocationNormalized } from 'vue-router'
import { baseName, currentTab, forgetTab, homeOf, LOCAL, neighbour, pathFor, rememberPath, serverTabs, setCurrentTab, tabGuard, tabHref } from './tabs'

const route = (name: string, params: Record<string, string> = {}, extra: Partial<RouteLocationNormalized> = {}) =>
  ({ name, params, query: {}, hash: '', meta: {}, ...extra }) as unknown as RouteLocationNormalized

const tab = (id: string) => ({ id, name: id, tag: 'development', color: '#000000', readOnly: false })

beforeEach(() => {
  setCurrentTab(LOCAL)
  serverTabs.value = []
})

describe('tabGuard', () => {
  it('leaves this computer’s pages alone on its tab', () => {
    expect(tabGuard(route('issues'))).toBe(true)
    expect(tabGuard(route('new'))).toBe(true)
  })
  it('keeps a server tab’s links on that server', () => {
    setCurrentTab('sabc')
    expect(tabGuard(route('issues'))).toEqual({ name: 'server-issues', params: { server: 'sabc' }, query: {}, hash: '' })
    expect(tabGuard(route('config', { path: '/etc/x.conf' }))).toMatchObject({ name: 'server-config', params: { path: '/etc/x.conf', server: 'sabc' } })
    // Doctor and Add new are this computer's: a server gets its report and dashboard.
    expect(tabGuard(route('doctor'))).toMatchObject({ name: 'server-report' })
    expect(tabGuard(route('new-form'))).toMatchObject({ name: 'server-dashboard' })
    // Settings belongs to no tab.
    expect(tabGuard(route('settings'))).toBe(true)
  })
  it('takes the tab from a server route', () => {
    expect(tabGuard(route('server-issues', { server: 'sxyz' }))).toBe(true)
    expect(currentTab.value).toBe('sxyz')
  })
  it('never touches the popover', () => {
    setCurrentTab('sabc')
    expect(tabGuard(route('tray', {}, { meta: { bare: true } }))).toBe(true)
  })
})

describe('tab paths', () => {
  it('prefixes a page with the tab on screen', () => {
    expect(tabHref('/issues')).toBe('/issues')
    setCurrentTab('sabc')
    expect(tabHref('/issues')).toBe('/s/sabc/issues')
    expect(tabHref('/')).toBe('/s/sabc')
    expect(homeOf(LOCAL)).toBe('/')
    expect(baseName('server-issues-log')).toBe('issues-log')
  })
  it('remembers each tab’s page', () => {
    expect(pathFor('sabc')).toBe('/s/sabc')
    rememberPath('sabc', '/s/sabc/issues')
    expect(pathFor('sabc')).toBe('/s/sabc/issues')
    serverTabs.value = [tab('sabc')]
    forgetTab('sabc')
    expect(pathFor('sabc')).toBe('/s/sabc')
    expect(serverTabs.value).toEqual([])
  })
  it('moves to the next and previous tab, wrapping round', () => {
    serverTabs.value = [tab('sa'), tab('sb')]
    expect(neighbour(1)).toBe('sa')
    expect(neighbour(-1)).toBe('sb')
    setCurrentTab('sb')
    expect(neighbour(1)).toBe(LOCAL)
  })
})
