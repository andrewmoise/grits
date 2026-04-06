// lib/rmdir/main.js
export const help = `\
rmdir — remove an empty directory

Usage:
  rmdir('path')          remove if empty, fail if not
  rmdir('path', {f:1})   remove even if not empty`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_IS_TREE } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('rmdir: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('rmdir: expected rmdir(path)');

  const { serverUrl, volume, path } = shell.resolvePath(positional[0]);
  const vol = shell._vol(serverUrl, volume);

  if (!opts.f) {
    const file = await vol.lookup(path);
    if (!file.isDir())
      throw new Error(`rmdir: not a directory: '${positional[0]}'`);
    const entries = await file.json();
    if (Object.keys(entries).length > 0)
      throw new Error(`rmdir: directory not empty: '${positional[0]}'`);
  }

  try {
    await vol.multiLink([{
      path,
      addr:   '',
      assert: opts.f ? 0 : ASSERT_IS_TREE,
    }]);
  } catch (e) {
    if (e instanceof AssertionError)
      throw new Error(`rmdir: not a directory: '${positional[0]}'`);
    throw e;
  }

  return VOID;
}