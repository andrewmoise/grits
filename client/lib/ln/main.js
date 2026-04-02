// lib/ln/main.js — link a GritsFile into the filesystem
//
// Usage:
//   <gritsfile>.ln('dest/path')        pipe a GritsFile in as source
//   ln('src/path', 'dest/path')        both as path strings
//   ln(gritsFile, 'dest/path')         explicit GritsFile source

export const help = `\
ln — link a file pointer into the filesystem

Usage:
  <gritsfile>.ln('dest/path')        GritsFile input, one path arg
  ln('src/path', 'dest/path')        void input, two path strings
  ln(gritsFile, 'dest/path')         void input, GritsFile + path string

ln() only moves pointers — it never uploads content.
Use to() if you want to write a bytestream to a path.`;

import { isVoid, VOID } from '../gimbal/gsh.js';
import { GritsFile, ASSERT_PREV_MATCHES, AssertionError } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  const hasInput = !isVoid(prev);

  let srcFile, destPath;

  if (hasInput) {
    // Pipeline mode: GritsFile input + one path arg
    if (!(prev instanceof GritsFile))
      throw new Error('ln: pipeline input must be a GritsFile — use to() for bytestreams');
    if (args.length !== 1 || typeof args[0] !== 'string')
      throw new Error('ln: expected exactly one destination path argument');
    srcFile  = prev;
    destPath = args[0];
  } else {
    // Argument mode: (GritsFile | string, string)
    if (args.length !== 2)
      throw new Error('ln: expected two arguments — ln(src, dest)');
    const [src, dest] = args;
    if (typeof dest !== 'string' || !dest)
      throw new Error('ln: destination must be a non-empty path string');
    destPath = dest;

    if (src instanceof GritsFile) {
      srcFile = src;
    } else if (typeof src === 'string') {
      const vol = shell._currentVol();
      srcFile = await vol.lookup(shell.resolvePath(src).replace(/^\//, ''));
    } else {
      throw new Error(`ln: source must be a GritsFile or path string, got ${src?.constructor?.name ?? typeof src}`);
    }
  }

  const resolved = shell.resolvePath(destPath).replace(/^\//, '');

  try {
    await vol.multiLink([{
      path:     resolved,
      addr:     srcFile.cid(),
      prevAddr: opts.f ? undefined : '',
      assert:   opts.f ? 0 : ASSERT_PREV_MATCHES,
    }]);
  } catch(e) {
    if (e instanceof AssertionError)
      throw new Error(`ln: already exists: '${destPath}' — use {f:1} to overwrite`);
    throw e;
  }

  return VOID;
}