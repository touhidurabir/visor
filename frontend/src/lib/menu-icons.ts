// One 24x24 stroke icon (SVG path data) per menu item id. Unknown ids get none.
const stop = 'M6 6h12v12H6z'
const restart = 'M20 12a8 8 0 1 1-2.5-5.8M20 4v5h-5'
const logs = 'M5 4h14v16H5zM8 9h8M8 13h8M8 17h5'
const check = 'M5 12l5 5 9-10'

export const menuIcons: Record<string, string> = {
  start: 'M7 4l13 8-13 8z',
  stop,
  restart,
  signal: 'M13 3L5 14h6l-1 7 8-11h-6z',
  logs,
  clear: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13',
  'copy-pid': 'M9 9h11v11H9zM5 15V4h11',
  'start-all': 'M5 4l11 8-11 8zM20 5v14',
  'stop-all': stop,
  'restart-all': restart,
  'open-config': 'M4 20h4L19 9l-4-4L4 16z',
  describe: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 11v5M12 8v.01',
  reveal: 'M3 6h6l2 2h10v11H3z',
  'open-log': 'M14 4h6v6M20 4l-9 9M18 14v6H4V6h6',
  // An arrow into a tray: supervisord takes the file in.
  load: 'M12 3v12M7 10l5 5 5-5M4 17v3h16v-3',
  apply: check,
  remove: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM8 12h8',
  check: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM21 21l-5-5',
  'apply-all': 'M2 12l5 5 8-10M13 17l1 1 8-10',
  log: logs,
  shutdown: 'M12 3v9M6.4 7a8 8 0 1 0 11.2 0',
  settings: 'M4 7h10M18 7h2M4 17h2M10 17h10M14 5v4M6 15v4',
}
