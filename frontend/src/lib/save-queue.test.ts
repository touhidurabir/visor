import { describe, expect, it } from 'vitest'
import { createSaveQueue } from './save-queue'

interface S {
  a: number
  b: number
}

/** A fake server: send resolves when the test calls release(). */
function server(start: S) {
  let saved = { ...start }
  const sent: S[] = []
  const waiting: Array<() => void> = []
  let failNext = false
  return {
    sent,
    saved: () => saved,
    failNext: () => (failNext = true),
    send(next: S): Promise<S> {
      sent.push({ ...next })
      return new Promise<S>((resolve, reject) => {
        waiting.push(() => {
          if (failNext) {
            failNext = false
            reject(new Error('disk full'))
            return
          }
          saved = { ...next, a: Math.min(next.a, 10) } // the server clamps a
          resolve({ ...saved })
        })
      })
    },
    async release() {
      await Promise.resolve()
      const w = waiting.shift()
      if (!w) throw new Error('nothing in flight')
      w()
      await new Promise((r) => setTimeout(r, 0))
    },
    inFlight: () => waiting.length,
  }
}

function setup(start: S = { a: 1, b: 1 }) {
  const srv = server(start)
  let view = { ...start }
  const done: Array<{ v: S | null; errors: unknown[] }> = []
  const q = createSaveQueue<S, S>({
    base: () => view,
    settingsOf: (v) => v,
    send: (next) => srv.send(next),
    done: (v, errors) => {
      done.push({ v, errors })
      if (v) view = v
    },
  })
  return { srv, q, done, view: () => view }
}

describe('createSaveQueue', () => {
  it('sends one change and reports the response once drained', async () => {
    const { srv, q, done, view } = setup()
    const p = q.enqueue((s) => (s.a = 5))
    expect(q.busy()).toBe(true)
    await srv.release()
    await p
    expect(srv.sent).toEqual([{ a: 5, b: 1 }])
    expect(done).toEqual([{ v: { a: 5, b: 1 }, errors: [] }])
    expect(view()).toEqual({ a: 5, b: 1 })
    expect(q.busy()).toBe(false)
  })

  it('never sends two saves at once, and a later save keeps the earlier change', async () => {
    const { srv, q, done } = setup()
    const p1 = q.enqueue((s) => (s.a = 5))
    const p2 = q.enqueue((s) => (s.b = 7)) // made before the first response
    expect(srv.inFlight()).toBe(1)
    await srv.release()
    expect(srv.inFlight()).toBe(1)
    await srv.release()
    await Promise.all([p1, p2])
    expect(srv.sent).toEqual([
      { a: 5, b: 1 },
      { a: 5, b: 7 }, // built on the first response, not the stale view
    ])
    expect(srv.saved()).toEqual({ a: 5, b: 7 })
    // Only the final response is handed back: no stale one in between.
    expect(done).toEqual([{ v: { a: 5, b: 7 }, errors: [] }])
  })

  it('coalesces changes made while a save is in flight into one send', async () => {
    const { srv, q } = setup()
    void q.enqueue((s) => (s.a = 2))
    void q.enqueue((s) => (s.a = 3))
    void q.enqueue((s) => (s.b = 4))
    await srv.release()
    await srv.release()
    expect(srv.sent).toEqual([
      { a: 2, b: 1 },
      { a: 3, b: 4 },
    ])
  })

  it('builds on what the server kept (clamped), not what was typed', async () => {
    const { srv, q } = setup()
    void q.enqueue((s) => (s.a = 99))
    void q.enqueue((s) => (s.b = 2))
    await srv.release()
    await srv.release()
    expect(srv.sent[1]).toEqual({ a: 10, b: 2 })
  })

  it('drops a failed change, keeps going, and reports the error', async () => {
    const { srv, q, done } = setup()
    srv.failNext()
    const p = q.enqueue((s) => (s.a = 5))
    void q.enqueue((s) => (s.b = 6))
    await srv.release()
    await srv.release()
    await p
    expect(srv.sent[1]).toEqual({ a: 1, b: 6 }) // a=5 never saved
    expect(done).toHaveLength(1)
    expect(done[0].v).toEqual({ a: 1, b: 6 })
    expect(String(done[0].errors[0])).toContain('disk full')
  })

  it('reports no response when every save failed', async () => {
    const { srv, q, done } = setup()
    srv.failNext()
    const p = q.enqueue((s) => (s.a = 5))
    await srv.release()
    await p
    expect(done).toEqual([{ v: null, errors: [expect.any(Error)] }])
  })

  it('starts a new run from the latest view after draining', async () => {
    const { srv, q, view } = setup()
    const p = q.enqueue((s) => (s.a = 5))
    await srv.release()
    await p
    const p2 = q.enqueue((s) => (s.b = 9))
    await srv.release()
    await p2
    expect(srv.sent[1]).toEqual({ a: 5, b: 9 })
    expect(view()).toEqual({ a: 5, b: 9 })
  })
})
