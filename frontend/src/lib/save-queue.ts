// A save queue for settings that save as they change. Go's Save takes the
// whole settings struct, so two overlapping saves built from the same view
// would let the second undo the first. The queue keeps one save in flight,
// folds changes made meanwhile into the next one, builds each from the
// latest response (what Go kept, after clamping), and hands back only the
// final response once nothing is left, so a stale one never lands over a
// newer change.

export interface SaveQueueOptions<S, V> {
  /** The latest saved settings the page knows, read when a run starts. */
  base: () => S
  /** The settings inside a response. */
  settingsOf: (v: V) => S
  send: (next: S) => Promise<V>
  /**
   * Called once the queue drains: the last successful response (null if
   * none succeeded) and the errors of failed saves, whose changes are
   * dropped.
   */
  done: (v: V | null, errors: unknown[]) => void
  /** Copies settings before a change edits them; JSON by default. */
  clone?: (s: S) => S
}

export interface SaveQueue<S> {
  /** Queues a change; resolves when the queue has drained. */
  enqueue(change: (next: S) => void): Promise<void>
  busy(): boolean
}

export function createSaveQueue<S, V>(o: SaveQueueOptions<S, V>): SaveQueue<S> {
  const clone = o.clone ?? ((s: S) => JSON.parse(JSON.stringify(s)) as S)
  let pending: Array<(next: S) => void> = []
  let running: Promise<void> | null = null

  async function drain() {
    let latest: S | null = null
    let last: V | null = null
    const errors: unknown[] = []
    while (pending.length > 0) {
      const batch = pending
      pending = []
      const next = clone(latest ?? o.base())
      for (const change of batch) change(next)
      try {
        last = await o.send(next)
        latest = o.settingsOf(last)
      } catch (e) {
        errors.push(e)
      }
    }
    running = null // before done: a change queued from done starts a new run
    o.done(last, errors)
  }

  return {
    enqueue(change) {
      pending.push(change)
      if (!running) running = drain()
      return running
    },
    busy: () => running !== null,
  }
}
