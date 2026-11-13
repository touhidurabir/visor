// Load: has supervisord add the programs of a config file it doesn't know
// yet (ConfigService.LoadFile, supervisorctl update for just those groups).
// Resolves to the sentence to show; throws when supervisord reported a
// problem, so callers show it as an error.
import { ConfigService } from './api'

export async function loadFile(path: string, fileName: string): Promise<string> {
  const res = await ConfigService.LoadFile(path)
  const errors = res.Errors ?? []
  if (errors.length) throw new Error(`supervisord couldn’t load ${fileName}: ${errors.join('; ')}`)
  const added = res.Added ?? []
  return added.length ? `Loaded ${added.join(', ')}.` : `supervisord already runs everything in ${fileName}.`
}
