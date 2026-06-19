// lib/logout/main.js
export const help = `\
logout — log out the current user

Usage:
  logout()                         — log out all users (clears session + cookie)
  logout('username')               — log out a specific user`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'logout') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length > 1)
    throw new Error(`${cmd}: too many arguments`);

  const username = positional[0] || undefined;
  await shell.fs.logout(shell.serverUrl, username);
  return VOID;
}
