import { VOID, _isPlainObject } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `iframe <url> — open a URL in an iframe widget`;

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const defaults   = WIDGET_ICONS.iframe;

  let url = typeof positional[0] === 'string' ? positional[0] : 'about:blank';

  if (url !== 'about:blank' && !/^https?:\/\//i.test(url)) {
    url = 'https://' + url;
  }

  const name = (() => {
    try { return new URL(url).hostname; } catch { return url; }
  })();

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name,
    icon:      opts.icon      ?? defaults.icon,
    iconColor: opts.iconColor ?? defaults.iconColor,
    zone: 'master',
    url,
  });

  return VOID;
}