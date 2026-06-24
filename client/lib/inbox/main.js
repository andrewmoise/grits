import { VOID, _isPlainObject } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `inbox — open inbox widget`;

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const defaults   = WIDGET_ICONS.inbox;

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: '',
    icon:      opts.icon      ?? defaults.icon,
    iconColor: opts.iconColor ?? defaults.iconColor,
    zone: 'master',
    shell,
    args: positional,
  });
  return VOID;
}
