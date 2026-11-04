import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { dismissToast, runToastAction, showToast, toastLifetime, toasts } from './toast'

beforeEach(() => {
  vi.useFakeTimers()
  toasts.value = []
})
afterEach(() => vi.useRealTimers())

describe('toast', () => {
  it('keeps the last three and dismisses info after 4 s', () => {
    for (const t of ['a', 'b', 'c', 'd']) showToast(t)
    expect(toasts.value.map((t) => t.text)).toEqual(['b', 'c', 'd'])
    vi.advanceTimersByTime(4000)
    expect(toasts.value).toEqual([])
  })

  it('keeps errors and toasts with an action for 8 s', () => {
    expect(toastLifetime('info')).toBe(4000)
    expect(toastLifetime('error')).toBe(8000)
    expect(toastLifetime('info', { label: 'Open Doctor', run: () => {} })).toBe(8000)
    showToast('shut down', 'info', { label: 'Open Doctor', run: () => {} })
    vi.advanceTimersByTime(4000)
    expect(toasts.value).toHaveLength(1)
    vi.advanceTimersByTime(4000)
    expect(toasts.value).toEqual([])
  })

  it('has no action unless given one', () => {
    showToast('plain')
    expect(toasts.value[0]).not.toHaveProperty('action')
  })

  it('runs the action once and dismisses the toast', () => {
    const run = vi.fn()
    showToast('shut down', 'info', { label: 'Open Doctor', run })
    const { id } = toasts.value[0]
    runToastAction(id)
    expect(run).toHaveBeenCalledTimes(1)
    expect(toasts.value).toEqual([])
    runToastAction(id) // already gone: nothing happens
    expect(run).toHaveBeenCalledTimes(1)
  })

  it('dismissing does not run the action', () => {
    const run = vi.fn()
    showToast('shut down', 'info', { label: 'Open Doctor', run })
    dismissToast(toasts.value[0].id)
    expect(run).not.toHaveBeenCalled()
  })
})
