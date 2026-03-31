// lib/cp/main.js
export const help = `\
cp — copy a file to a new path

Usage:
  cp('src', 'dest')          copy, fail if dest exists
  cp('src', 'dest', {f:1})   overwrite dest
  <gritsfile>.cp('dest')     pipe source in`;

import { isVoid, VOID } from '../gimbal/gsh.js';
import { GritsFile, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObj(args[args.length-1]) ? args[args.length-1] : {};
  const positional = opts === args[args.length-1] ? args.slice(0,-1) : [...args];
  const prev       = await previous;
  const hasInput   = !isVoid(prev);

  let srcFile, destPath;
  const vol = shell._currentVol();

  if (hasInput) {
    if (!(prev instanceof GritsFile))
      throw new Error('cp: pipeline input must be a GritsFile');
    if (positional.length !== 1 || typeof positional[0] !== 'string')
      throw new Error('cp: expected exactly one destination path argument');
    srcFile  = prev;
    destPath = positional[0];
  } else {
    if (positional.length !== 2 || typeof positional[0] !== 'string' || typeof positional[1] !== 'string')
      throw new Error('cp: expected cp(src, dest)');
    srcFile  = await vol.lookup(shell.resolvePath(positional[0]).replace(/^\//, ''));
    destPath = positional[1];
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
        throw new Error(`cp: destination already exists: '${destPath}' — use {f:1} to overwrite`);
    throw e;
  }

  return VOID;
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}