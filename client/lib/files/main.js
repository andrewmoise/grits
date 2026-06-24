import { VOID, _isPlainObject } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `files [path] — open file browser widget`;

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const defaults   = WIDGET_ICONS.files;

  const path  = typeof positional[0] === 'string' ? positional[0] : null;
  const name  = path ? path.split('/').filter(Boolean).pop() || '/' : 'files';

  let r = null;
  if (path) {
    try { r = shell.resolvePath(path); } catch {}
  }

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name,
    icon:      opts.icon      ?? defaults.icon,
    iconColor: opts.iconColor ?? defaults.iconColor,
    zone: 'master',
    shell,
    args,
  });

  return VOID;
}
