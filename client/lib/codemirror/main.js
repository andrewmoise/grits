import { VOID, _isPlainObject } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `edit [path] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const defaults   = WIDGET_ICONS.edit;

  const path = typeof positional[0] === 'string' ? positional[0] : null;
  const r    = path ? shell.resolvePath(path) : null;

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: '',
    icon:      opts.icon      ?? defaults.icon,
    iconColor: opts.iconColor ?? defaults.iconColor,
    zone: 'master',
    shell,
    file: r,
  });

  return VOID;
}
