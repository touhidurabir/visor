// Plain-English text for supervisord faults. Go formats a fault as
// "xmlrpc fault 60: ALREADY_STARTED" (possibly wrapped by the action that
// failed), and a connection failure as "supervisord: unreachable: …".
const MESSAGES: Record<string, string> = {
  BAD_NAME: 'There is no process with that name. Check for changes, then try again.',
  NOT_RUNNING: 'That process isn’t running.',
  ALREADY_STARTED: 'That process is already running.',
  SPAWN_ERROR: 'supervisord could not start the process. Check the command and the log for details.',
  ABNORMAL_TERMINATION: 'The process stopped right after starting. Check its log for the reason.',
  NO_FILE: 'supervisord can’t find the command. Check the command path in the config file.',
  NOT_EXECUTABLE: 'The command isn’t executable. Check its permissions.',
  UNREACHABLE: 'Can’t reach supervisord. Check that it is running, then try again.',
}

export function faultMessage(code: string): string {
  return MESSAGES[code] ?? `supervisord refused the request (${code}).`
}

/** describeError turns whatever a service call threw into a sentence for a toast. */
export function describeError(e: unknown): string {
  const text = e instanceof Error ? e.message : String(e)
  const fault = /xmlrpc fault \d+: ([A-Z_]+)/.exec(text)
  if (fault) return faultMessage(fault[1])
  if (/supervisord: unreachable/.test(text)) return faultMessage('UNREACHABLE')
  return text
}
