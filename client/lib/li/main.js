// lib/li/main.js — link a GritsFile or GritsCID into the filesystem
//
// Usage:
//   cat('a.txt').li('b.txt')              — re-link from stdin
//   li(lo('a.txt'), 'b.txt')              — explicit source, two-arg form
//   li(cid('Qm...'), 'b.txt')             — link a known CID
//   mkfile('hello').li('greeting.txt')    — content → file → link
//   lo('a.txt').li('b.txt')               — copy a pointer
//
// Input:  GritsFile | GritsCID  (from stdin OR first arg)
// Output: Path (the destination that was linked)
//
// Options (last arg, optional): none currently defined
//
// li() only moves pointers — it never uploads content.
// Use mkfile() first if you have raw content to write.

export const help = `\
li — link a file pointer into the filesystem

Usage:
  <source>.li('dest/path')          pipe a GritsFile/GritsCID in as source
  li(src, 'dest/path')              explicit source (GritsFile, GritsCID, or path string)

li() only links pointers; it never uploads content.
Use mkfile() to turn content into a linkable GritsFile first.`;

import { isVoid, coerceToFile, Path } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  let src, destPath;

  if (args.length >= 2) {
    // li(src, dest) — explicit source
    [src, destPath] = args;
  } else if (args.length === 1) {
    // li(dest) — source comes from stdin
    destPath = args[0];
    src = await previous;
    if (isVoid(src))
      throw new Error('li: no source — pipe a GritsFile/GritsCID in, or use li(src, dest)');
  } else {
    throw new Error('li: requires at least one argument (destination path)');
  }

  if (typeof destPath !== 'string' || !destPath)
    throw new Error('li: destination must be a non-empty path string');

  const resolvedDest = shell.resolvePath(destPath);
  const file         = await coerceToFile(src, shell);

  await shell._currentVol().li(file, resolvedDest);

  return new Path(resolvedDest, shell.serverUrl, shell.volume);
}