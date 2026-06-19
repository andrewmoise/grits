export const help = `\
message — send a message to a user's inbox

Usage:
  message('to', 'subject', 'body')
  message('to', 'subject', 'body', {bodyHtml: '<p>html</p>', ...})

The message is written as a JSON file to the recipient's local/inbox/.
Any extra keys in the options object are included in the message JSON.`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('message: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length < 3)
    throw new Error('message: expected message(to, subject, body, ...opts)');

  const to      = positional[0];
  const subject = positional[1];
  const body    = positional[2];

  if (typeof to !== 'string' || typeof subject !== 'string' || typeof body !== 'string')
    throw new Error('message: to, subject, and body must be strings');

  const identities = await shell.fs.whoami(shell.serverUrl);
  const from = identities?.[0]?.username || '(anonymous)';

  const msg = {
    from,
    to,
    subject,
    body,
    timestamp: new Date().toISOString(),
  };
  if (opts.bodyHtml) msg.bodyHtml = opts.bodyHtml;
  if (opts.bodyMarkdown) msg.bodyMarkdown = opts.bodyMarkdown;
  for (const [k, v] of Object.entries(opts)) {
    if (k === 'bodyHtml' || k === 'bodyMarkdown') continue;
    msg[k] = v;
  }

  const bytes = new TextEncoder().encode(JSON.stringify(msg));
  const destPath = `home/${to}/local/inbox`;
  const vol = shell._vol(shell.serverUrl, 'primary');

  const contentCID = await vol.put(bytes);
  const metaCID = await vol.mkfile(contentCID, bytes.byteLength);

  const maxAttempts = 20;
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    const filename = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}.json`;
    try {
      await vol.multiLink([{
        path: `${destPath}/${filename}`,
        addr: metaCID,
        prevAddr: '',
        assert: ASSERT_PREV_MATCHES,
      }]);
      return VOID;
    } catch (e) {
      if (!(e instanceof AssertionError)) throw e;
    }
  }

  throw new Error('message: could not find a unique filename after 20 attempts');
}
