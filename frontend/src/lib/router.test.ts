import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { configRoute } from './links'
import { createAppRouter } from './router'

const resolve = (path: string) => createAppRouter(createMemoryHistory()).resolve(path)

describe('routes', () => {
  it.each(['/', '/issues', '/new', '/new/form', '/settings', '/doctor'])('%s resolves inside the shell', (path) => {
    const r = resolve(path)
    expect(r.matched).toHaveLength(1)
    expect(r.meta.bare).toBeUndefined()
  })
  it('/tray is bare so it renders outside AppShell', () => {
    const r = resolve('/tray')
    expect(r.name).toBe('tray')
    expect(r.meta.bare).toBe(true)
  })
  it('an unknown path redirects to the dashboard', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/nope/here')
    expect(router.currentRoute.value.name).toBe('dashboard')
  })
  it('/config/:path keeps slashes in the path', () => {
    expect(resolve('/config/conf.d/mailer.conf').params.path).toBe('conf.d/mailer.conf')
  })
  it('an absolute config path round-trips through configRoute', async () => {
    const router = createAppRouter(createMemoryHistory())
    const path = '/tmp/visor sandbox/conf.d/mailer#1.ini'
    await router.push(configRoute(path, { tab: 'logs', process: 'mailer-worker:mailer-worker_00' }))
    const r = router.currentRoute.value
    expect(r.name).toBe('config')
    expect(r.params.path).toBe(path)
    expect(r.query).toEqual({ tab: 'logs', process: 'mailer-worker:mailer-worker_00' })
    // and survives a reload from the URL alone
    expect(resolve(r.fullPath).params.path).toBe(path)
  })
  it('a hand-written /config//abs/path link still finds the absolute path', () => {
    expect(resolve('/config//tmp/conf.d/mailer.ini').params.path).toBe('/tmp/conf.d/mailer.ini')
  })
})
