// lib/mv/main.js
export const help = `\
mv — move (rename) a file

Usage:
  mv('src', 'dest')          move, fail if dest exists
  mv('src', 'dest', {f:1})   overwrite dest`;

import { isVoid, VOID } from '../gimbal/gsh.js';
import { ASSERT_PREV_MATCHES, ASSERT_IS_NONEMPTY } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('mv: does not accept pipeline input');

  const opts       = _isPlainObj(args[args.length-1]) ? args[args.length-1] : {};
  const positional = opts === args[args.length-1] ? args.slice(0,-1) : [...args];

  if (positional.length !== 2 || typeof positional[0] !== 'string' || typeof positional[1] !== 'string')
    throw new Error('mv: expected mv(src, dest)');

  const vol      = shell._currentVol();
  const srcPath  = shell.resolvePath(positional[0]).replace(/^\//, '');
  const destPath = shell.resolvePath(positional[1]).replace(/^\//, '');

  // Look up source first
  const srcFile = await vol.lookup(srcPath);

  // Atomically: assert src still exists, set dest, clear src
  try {
    await vol.multiLink([
      {
        path:     destPath,
        addr:     srcFile.cid(),
        prevAddr: opts.f ? undefined : '',
        assert:   opts.f ? 0 : ASSERT_PREV_MATCHES,
      },
      {
        path:     srcPath,
        addr:     '',
        prevAddr: srcFile.cid(),
        assert:   ASSERT_PREV_MATCHES,
      },
    ]);
  } catch(e) {
    if (e instanceof AssertionError)
        throw new Error(`mv: destination already exists or source changed — use {f:1} to overwrite`);
    throw e;
  }
  
  return VOID;
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}