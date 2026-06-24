export const help = `\
message — send a message to a user's inbox

Usage:
  message('to', 'subject', 'body')
  message('to', 'body')
  message()

With to and body provided, sends directly. Otherwise opens a compose widget.
Any extra keys in the options object are included in the message JSON.`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { sendMessage } from './send.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('message: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const to = positional[0];
  const body = positional.length >= 2 ? positional[positional.length - 1] : undefined;

  if (typeof to !== 'string' || typeof body !== 'string') {
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name: '',
      icon: 'message',
      zone: 'master',
      shell,
      args: [{ to: positional[0] || '', subject: positional[1] || '' }],
    });
    return VOID;
  }

  const subject = positional.length > 2 ? positional[1] : '';
  const identities = await shell.fs.whoami(shell.serverUrl);
  const from = identities?.[0]?.username || '(anonymous)';
  const vol = shell._vol(shell.serverUrl, 'primary');

  await sendMessage(vol, to, from, subject, body, opts);
  return VOID;
}
