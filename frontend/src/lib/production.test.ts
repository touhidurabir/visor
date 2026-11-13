import { beforeEach, describe, expect, it } from 'vitest'
import { guard, isCancelled, productionAsk, withConsent } from './production'
import { serverTabs } from './tabs'

const flush = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  productionAsk.value = null
  serverTabs.value = [
    { id: 'sprod', name: 'cpcsl prod', tag: 'production', color: '#E5483A', readOnly: false },
    { id: 'sdev', name: 'cpcsl dev', tag: 'development', color: '#0A62D0', readOnly: false },
  ]
})

describe('production guard', () => {
  it('asks on a production tab, naming the server, and goes ahead on yes', async () => {
    const p = guard('sprod', 'Restart', ['web:web'])
    await flush()
    expect(productionAsk.value?.title).toBe('Restart web:web on cpcsl prod?')
    productionAsk.value!.answer(true)
    await expect(p).resolves.toBeUndefined()
  })
  it('a no stops the call quietly', async () => {
    const p = guard('sprod', 'StopAll', [])
    await flush()
    productionAsk.value!.answer(false)
    await expect(p).rejects.toSatisfy(isCancelled)
  })
  it('one answer covers every call of an action, also after a wait', async () => {
    await withConsent(async () => {
      await guard('sprod', 'StopGroup', ['a'])
      await flush()
      await guard('sprod', 'StopGroup', ['b'])
    })
    expect(productionAsk.value).toBeNull()
    // …and only while it runs.
    const p = guard('sprod', 'StopGroup', ['c'])
    await flush()
    expect(productionAsk.value).not.toBeNull()
    productionAsk.value!.answer(false)
    await expect(p).rejects.toSatisfy(isCancelled)
  })
  it('never asks elsewhere, for calls that don’t interrupt, or after a dialog asked', async () => {
    await guard('sdev', 'Restart', ['x'])
    await guard('local', 'StopAll', [])
    await guard('sprod', 'Start', ['x'])
    await withConsent(() => guard('sprod', 'ClearLogs', ['x']))
    expect(productionAsk.value).toBeNull()
  })
})
