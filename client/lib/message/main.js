import { GimbalResult } from '../gimbal/result.js';
import { WIDGET_ICONS } from '../style/icons.js';
import { sendMessage } from './send.js';

export const help = `\
message — send a message to a user's inbox

Usage:
  gimbal.message('to', 'subject', 'body')
  gimbal.message('to', 'body')
  gimbal.message()

With to and body provided, sends directly. Otherwise opens a compose widget.`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export function invoke(gimbal, prev, ...args) {

  const rawOpts = isPlainObject(args[args.length - 1]) ? args.pop() : {};
  const positional = args;
  const defaults = WIDGET_ICONS.message;
  const { icon, iconColor, ...messageOpts } = rawOpts;

  const to = positional[0];
  const body = positional.length >= 2 ? positional[positional.length - 1] : undefined;

  if (typeof to !== 'string' || typeof body !== 'string') {
    return new GimbalResult(async () => {
      const mod = await import('./gwm-widget.js');
      await window.gimbal.openWidget(mod, {
        name: '',
        icon: icon ?? defaults.icon,
        iconColor: iconColor ?? defaults.iconColor,
        zone: 'master',
        gimbal,
        args: [{ to: positional[0] || '', subject: positional[1] || '' }],
      });
    });
  }

  return new GimbalResult(async () => {
    const subject = positional.length > 2 ? positional[1] : '';
    const identities = await gimbal.grits.whoami(gimbal._serverUrl);
    const from = identities?.[0]?.username || '(anonymous)';
    const vol = gimbal.grits.volume(gimbal._serverUrl, 'primary');
    await sendMessage(vol, to, from, subject, body, messageOpts);
  });
}
