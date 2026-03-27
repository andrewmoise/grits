// lib/cat/main.js — read a file from the filesystem, return its Response

export const help = `\
cat — read a file from the filesystem

Usage:
  cat('path/to/file')
  cat('path/to/file', { volume: 'other', serverUrl: 'https://...' })

Input:  void (cat reads from the filesystem, not stdin)
Output: Response`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const [pathArg, opts = {}] = args;

  const prev = await previous;
  if (!isVoid(prev))
    console.warn('cat: ignoring piped input (cat always reads from the filesystem)');

  if (!pathArg || typeof pathArg !== 'string')
    throw new Error('cat: first argument must be a file path string');

  const vol  = shell._vol(opts.serverUrl ?? null, opts.volume ?? null);
  const file = await vol.lo(shell.resolvePath(pathArg));

  if (file.isDir()) throw new Error(`cat: ${pathArg}: is a directory`);

  return file.get();
}