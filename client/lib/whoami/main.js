// lib/whoami/main.js
export const help = `\
whoami — show identity info from the server

Usage:
  whoami()                   — bare quoted username per line (JSONL)
  whoami({v:1})              — verbose: ["username", status, "expiry"] per line

When not logged in, output is empty.`;

import { VOID, isVoid, responseFromText, _isPlainObject } from '../gimbal/gsh.js';

function formatExpiry(ts) {
  if (!ts) return 'never';
  const locale = typeof navigator !== 'undefined' ? (navigator.language || 'en-US') : 'en-US';
  return new Date(ts * 1000).toLocaleString(locale, { timeZoneName: 'short' });
}

export async function invoke(shell, previous, args, cmd = 'whoami') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  if (args.length > 1)
    throw new Error(`${cmd}: too many arguments`);

  const opts = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};

  const identities = await shell.fs.whoami(shell.serverUrl);
  if (!identities || identities.length === 0) {
    return VOID;
  }

  const verbose = !!opts.v;
  const lines = identities.map(id =>
    verbose
      ? JSON.stringify([id.username, id.status, formatExpiry(id.expiry)])
      : JSON.stringify(id.username)
  ).join('\n');
  return responseFromText(lines);
}
