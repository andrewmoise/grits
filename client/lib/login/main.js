// lib/login/main.js
export const help = `\
login — authenticate with a username and password

Usage:
  login()                                              — prompt for username and password
  login('username')                                    — prompt for password
  login('username', 'password')                        — direct
  login('username', 'password', {g:1})                 — also set global cookie
  login({guest:1})                                     — anonymous guest session
  login({guest:1, g:1})                                — anonymous guest, global cookie`;

import { VOID, isVoid, responseFromText, _isPlainObject } from '../gimbal/gsh.js';
import { promptPassword, promptCredentials } from '../gimbal/dialog.js';

export async function invoke(shell, previous, args, cmd = 'login') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  let [username, password] = positional;

  if (opts.guest) {
    await shell.fs.login(shell.serverUrl, '', '', { guest: true, global: !!opts.g });
    return VOID;
  }

  if (positional.length === 0) {
    const creds = await promptCredentials({ message: `Login to ${shell.serverUrl}` });
    if (!creds) return VOID;
    username = creds.username;
    password = creds.password;
  } else if (positional.length === 1) {
    password = await promptPassword({ message: `Password for ${username}:` });
    if (!password) return VOID;
  }

  if (!username || !password)
    throw new Error(`${cmd}: username and password are required`);

  await shell.fs.login(shell.serverUrl, username, password, { global: !!opts.g });
  return VOID;
}
