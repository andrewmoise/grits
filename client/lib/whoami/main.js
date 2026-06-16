// lib/whoami/main.js
export const help = `\
whoami — show identity info from the server

Output is JSONL: one JSON array per identity:
  ["username", status, "expiry_date"]`;

import { isVoid, responseFromText } from '../gimbal/gsh.js';

function formatExpiry(ts) {
  if (!ts) return 'never';
  const locale = typeof navigator !== 'undefined' ? (navigator.language || 'en-US') : 'en-US';
  return new Date(ts * 1000).toLocaleString(locale, { timeZoneName: 'short' });
}

export async function invoke(shell, previous, args, cmd = 'whoami') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  if (args.length > 0)
    throw new Error(`${cmd}: too many arguments`);

  const identities = await shell.fs.whoami(shell.serverUrl);
  if (!identities || identities.length === 0) {
    return responseFromText('(anonymous)');
  }
  const lines = identities.map(id =>
    JSON.stringify([id.username, id.status, formatExpiry(id.expiry)])
  ).join('\n');
  return responseFromText(lines);
}
