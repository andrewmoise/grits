import { GimbalResult } from '../gimbal/result.js';
import { promptPassword, promptCredentials } from '../gimbal/dialog.js';

export const help = `\
login — authenticate with a username and password

Usage:
  gimbal.login()                                           — prompt for username and password
  gimbal.login('username')                                 — prompt for password
  gimbal.login('username', 'password')                     — direct
  gimbal.login('username', 'password', {g:1})              — also set global cookie
  gimbal.login({guest:1})                                  — anonymous guest session
  gimbal.login({guest:1, g:1})                             — anonymous guest, global cookie`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export function invoke(gimbal, prev, ...args) {
  const last = args[args.length - 1];
  const opts = isPlainObject(last) ? args.pop() : {};
  const [username, password] = args;

  return new GimbalResult(async () => {
    if (opts.guest) {
      await gimbal.grits.login(gimbal._serverUrl, '', '', { guest: true, global: !!opts.g });
      return;
    }

    let u = username, p = password;

    if (u === undefined) {
      const creds = await promptCredentials({ message: `Login to ${gimbal._serverUrl}` });
      if (!creds) return;
      u = creds.username;
      p = creds.password;
    } else if (p === undefined) {
      p = await promptPassword({ message: `Password for ${u}:` });
      if (!p) return;
    }

    if (!u || !p) throw new Error('login: username and password are required');
    await gimbal.grits.login(gimbal._serverUrl, u, p, { global: !!opts.g });
  });
}
