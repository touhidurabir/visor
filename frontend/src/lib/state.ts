// How a supervisord state reads on screen: a sentence-case word, the tone of
// its dot (and badge tint), and whether the word itself is emphasised. The
// word is always shown, so colour is never the only signal. `status` is the
// Go side's classification (ProcessView.Status: "running", "failing",
// "stopped" or "other"); it tells an EXITED that wasn't expected (failing)
// from one that was (stopped). Without it the state name decides.

export type StateTone = 'ok' | 'warn' | 'bad' | 'idle'

export interface StateLook {
  label: string
  tone: StateTone
  /** A failing state: the word is coloured and semibold. */
  alert: boolean
}

/** "RUNNING" -> "Running", "BACKOFF" -> "Backoff". */
export function stateLabel(state: string): string {
  return state ? state.charAt(0).toUpperCase() + state.slice(1).toLowerCase() : ''
}

function stateTone(state: string, status?: string): StateTone {
  if (state === 'STARTING' || state === 'STOPPING' || state === 'BACKOFF') return 'warn'
  if (status === 'failing' || state === 'FATAL') return 'bad'
  if (status === 'running' || state === 'RUNNING') return 'ok'
  return 'idle'
}

export function stateLook(state: string, status?: string): StateLook {
  const tone = stateTone(state, status)
  return { label: stateLabel(state), tone, alert: tone === 'bad' || state === 'BACKOFF' }
}
