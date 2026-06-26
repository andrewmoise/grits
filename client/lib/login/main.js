import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { promptPassword, promptCredentials } from '../gimbal/dialog.js';

export const help = `\
login — authenticate with a username and password

Usage:
  gsh.login()                                           — prompt for username and password
  gsh.login('username')                                 — prompt for password
  gsh.login('username', 'password')                     — direct
  gsh.login('username', 'password', {g:1})              — also set global cookie
  gsh.login({guest:1})                                  — anonymous guest session
  gsh.login({guest:1, g:1})                             — anonymous guest, global cookie`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export function invoke(prev, ...args) {
  if (!(prev instanceof GimbalShell)) throw new Error('login: must be called on gsh');

  const shell = prev;
  const last = args[args.length - 1];
  const opts = isPlainObject(last) ? args.pop() : {};
  const [username, password] = args;

  return new GimbalResult(async () => {
    if (opts.guest) {
      await shell.fs.login(shell.serverUrl, '', '', { guest: true, global: !!opts.g });
      return;
    }

    let u = username, p = password;

    if (u === undefined) {
      const creds = await promptCredentials({ message: `Login to ${shell.serverUrl}` });
      if (!creds) return;
      u = creds.username;
      p = creds.password;
    } else if (p === undefined) {
      p = await promptPassword({ message: `Password for ${u}:` });
      if (!p) return;
    }

    if (!u || !p) throw new Error('login: username and password are required');
    await shell.fs.login(shell.serverUrl, u, p, { global: !!opts.g });
  });
}
