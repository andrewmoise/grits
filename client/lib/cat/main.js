// lib/cat/main.js
export const help = `\
cat — read a file and output its text content

Usage:
  cat('path/to/file')    read a file by path

Requires void input and exactly one path argument.`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { coerceToFile } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('cat: does not accept pipeline input — provide a path argument');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pathArg]  = positional;

  if (!pathArg || typeof pathArg !== 'string')
    throw new Error('cat: path argument required');

  const file = await coerceToFile(pathArg, shell);
  return file.text();
}