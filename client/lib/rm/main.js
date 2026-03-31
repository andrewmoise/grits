// lib/rm/main.js
export const help = `\
rm — remove a file or directory

Usage:
  rm('path')          remove file, fail if it's a directory
  rm('path', {f:1})   remove unconditionally (file or directory)
  rm('path', {i:1})   prompt before removing (not yet implemented)`;

import { isVoid, VOID } from '../gimbal/gsh.js';
import { ASSERT_IS_BLOB, AssertionError } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('rm: does not accept pipeline input');

  const opts       = _isPlainObj(args[args.length-1]) ? args[args.length-1] : {};
  const positional = opts === args[args.length-1] ? args.slice(0,-1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('rm: expected rm(path)');

  const vol      = shell._currentVol();
  const resolved = shell.resolvePath(positional[0]).replace(/^\//, '');

  try {
    await vol.multiLink([{ path: resolved, addr: '', assert: opts.f ? 0 : ASSERT_IS_BLOB }]);
  } catch(e) {
    if (e instanceof AssertionError)
        throw new Error(`rm: '${positional[0]}' is a directory — use rmdir or rm({f:1})`);
    throw e;
  }

  return VOID;
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}