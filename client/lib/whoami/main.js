import { GimbalClient } from '../gimbal/client.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
whoami — show identity info from the server

Usage:
  gimbal.whoami()                   — bare quoted username per line (JSONL)
  gimbal.whoami({v:1})              — verbose: ["username", status, "expiry"] per line

When not logged in, output is empty.`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

function formatExpiry(ts) {
  if (!ts) return 'never';
  const locale = typeof navigator !== 'undefined' ? (navigator.language || 'en-US') : 'en-US';
  return new Date(ts * 1000).toLocaleString(locale, { timeZoneName: 'short' });
}

export function invoke(gimbal, prev, opts) {
  if (!(prev instanceof GimbalClient)) throw new Error('whoami: must be called on gimbal');
  if (opts !== undefined && (typeof opts !== 'object' || opts instanceof GimbalPath || Array.isArray(opts)))
    throw new Error('whoami: options must be a plain object');
  opts = opts || {};
  return new GimbalResult(async () => {
    const identities = await gimbal.grits.whoami(gimbal._serverUrl);
    if (!identities || identities.length === 0) return null;

    const verbose = !!opts.v;
    return identities.map(id =>
      verbose
        ? JSON.stringify([id.username, id.status, formatExpiry(id.expiry)])
        : JSON.stringify(id.username)
    ).join('\n');
  });
}
