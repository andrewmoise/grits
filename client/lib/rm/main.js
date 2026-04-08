// lib/rm/main.js
export const help = `\
rm — remove files or directories

Usage:
  rm('path')              remove file, fail if it's a directory
  rm('a', 'b', ...)       remove multiple paths
  rm('path', {f:1})       remove unconditionally (file or directory)
  rm('a', 'b', {f:1})     remove multiple paths unconditionally`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('rm: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length < 1 || positional.some(a => typeof a !== 'string'))
    throw new Error('rm: expected rm(path, ...)');

  for (const arg of positional) {
    const { serverUrl, volume, path } = shell.resolvePath(arg);
    try {
      await shell._vol(serverUrl, volume).multiLink([{
        path,
        addr:   '',
        assert: opts.f ? 0 : ASSERT_IS_BLOB,
      }]);
    } catch (e) {
      if (e instanceof AssertionError)
        throw new Error(`rm: '${arg}' is a directory — use rmdir or rm({f:1})`);
      throw e;
    }
  }

  return VOID;
}