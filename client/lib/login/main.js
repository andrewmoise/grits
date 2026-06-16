// lib/login/main.js
export const help = `\
login — authenticate with a username and password

Usage:
  login('username', 'password')
  login('username', 'password', {g:1})   — also set global cookie`;

import { isVoid, responseFromText, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'login') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [username, password] = positional;
  if (!username || !password)
    throw new Error(`${cmd}: usage: login('username', 'password' [, {g:1}])`);

  await shell.fs.login(shell.serverUrl, username, password, { global: !!opts.g });
  return responseFromText(`logged in as ${username}`);
}
