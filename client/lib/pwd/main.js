// lib/pwd/main.js
export const help = `\
pwd — print current working directory

Usage:
  pwd()`;

import { isVoid, _isPlainObject, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'pwd') {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: cannot use pipeline input`);

  if (positional.length > 0)
    throw new Error(`${cmd}: too many arguments`);

  // FIXME - serverUrl
  const cwd = (shell.cwd == '/' ? '' : shell.cwd);
  const result = `//${shell._currentVol()._volume}/${cwd}`;
  return responseFromText(result);
}
