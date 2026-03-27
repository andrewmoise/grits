// lib/lo/main.js — look up a path, return a GritsFile (no content fetch)

export const help = `\
lo — look up a path, return a GritsFile handle (no content fetched)

Usage:
  lo('path/to/file')
  lo('path/to/file', { volume: 'other', serverUrl: 'https://...' })

Input:  void
Output: GritsFile

Use lo() when you want the file handle (for li, toCID, metadata inspection)
without fetching content. Use cat() when you want the content directly.`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const [pathArg, opts = {}] = args;

  const prev = await previous;
  if (!isVoid(prev))
    console.warn('lo: ignoring piped input');

  if (!pathArg || typeof pathArg !== 'string')
    throw new Error('lo: first argument must be a path string');

  const vol = shell._vol(opts.serverUrl ?? null, opts.volume ?? null);
  return vol.lo(shell.resolvePath(pathArg));
}