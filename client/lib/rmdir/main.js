// lib/rmdir/main.js
export const help = `\
rmdir — remove an empty directory

Usage:
  rmdir('path')          remove if empty, fail if not
  rmdir('path', {f:1})   remove even if not empty`;

import { isVoid, VOID } from '../gimbal/gsh.js';
import { ASSERT_IS_TREE, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('rmdir: does not accept pipeline input');

  const opts       = _isPlainObj(args[args.length-1]) ? args[args.length-1] : {};
  const positional = opts === args[args.length-1] ? args.slice(0,-1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('rmdir: expected rmdir(path)');

  const vol      = shell._currentVol();
  const resolved = shell.resolvePath(positional[0]).replace(/^\//, '');

  if (!opts.f) {
    // Check it's a directory and is empty
    const file = await vol.lookup(resolved);
    if (!file.isDir())
      throw new Error(`rmdir: not a directory: ${positional[0]}`);
    const entries = await file.json();
    if (Object.keys(entries).length > 0)
      throw new Error(`rmdir: directory not empty: ${positional[0]}`);
  }

  await vol.multiLink([{
    path:   resolved,
    addr:   '',
    assert: opts.f ? 0 : ASSERT_IS_TREE,
  }]);

  return VOID;
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}