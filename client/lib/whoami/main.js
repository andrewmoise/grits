// lib/whoami/main.js
export const help = `\
whoami — show the current username

Usage:
  whoami()`;

import { isVoid, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'whoami') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  if (args.length > 0)
    throw new Error(`${cmd}: too many arguments`);

  const user = shell.fs.whoami();
  return responseFromText(user || '(anonymous)');
}
