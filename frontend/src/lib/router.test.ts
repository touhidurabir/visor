import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
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
  it('/config/:path keeps slashes in the path', () => {
    expect(resolve('/config/conf.d/mailer.conf').params.path).toBe('conf.d/mailer.conf')
  })
})
