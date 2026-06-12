// lib/logout/main.js
export const help = `\
logout — log out the current user

Usage:
  logout()`;

import { isVoid, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'logout') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  if (args.length > 0)
    throw new Error(`${cmd}: too many arguments`);

  shell.fs.logout();
  return responseFromText('logged out');
}
