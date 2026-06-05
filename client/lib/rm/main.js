// lib/rm/main.js
export const help = `\
rm — remove files or directories

Usage:
  rm('path')              remove file, fail if it's a directory
  rm('a', 'b', ...)       remove multiple paths
  rm('path', {r:1})       remove file or directory unconditionally
  rm('path', {f:1})       ignore missing paths, but fail on directories
  rm('path', {r:1,f:1})   remove anything without complaint
  rm('a', 'b', {r:1})     remove multiple paths unconditionally`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_IS_BLOB, ASSERT_IS_TREE, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('rm: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length < 1 || positional.some(a => typeof a !== 'string'))
    throw new Error('rm: expected rm(path, ...)');

  for (const arg of positional) {
    const { serverUrl, volume, path } = shell.resolvePath(arg);

    // Assertion matrix:
    // default        -> ASSERT_IS_BLOB (fail on dir, fail on missing)
    // {f:1}          -> try BLOB or VOID (ignore missing, still fail on dir)
    // {r:1}          -> no assertion (allow file/dir, but fail on missing)
    // {r:1,f:1}      -> no assertion (allow anything, ignore missing)

    if (opts.r && opts.f) {
      await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: 0 }]);
    } else if (opts.r) {
      // allow file or directory, but require existence
      try {
        await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_IS_TREE }]);
      } catch (e) {
        if (e instanceof AssertionError) {
          try {
            await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_IS_BLOB }]);
          } catch (e2) {
            if (e2 instanceof AssertionError) {
              throw e2;
            }
            throw e2;
          }
        } else {
          throw e;
        }
      }
    } else if (opts.f) {
      try {
        await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_IS_BLOB }]);
      } catch (e) {
        if (e instanceof AssertionError) {
          try {
            await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_PREV_MATCHES, prevAddr: '' }]);
          } catch (e2) {
            if (e2 instanceof AssertionError)
              throw new Error(`rm: '${arg}' is a directory — use rmdir or rm({r:1})`);
            throw e2;
          }
        } else {
          throw e;
        }
      }
    } else {
      try {
        await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_IS_BLOB }]);
      } catch (e) {
        if (e instanceof AssertionError) {
          try {
            await shell._vol(serverUrl, volume).multiLink([{ path, addr: '', assert: ASSERT_PREV_MATCHES, prevAddr: '' }]);
          } catch (e2) {
            if (e2 instanceof AssertionError)
              throw new Error(`rm: '${arg}' is a directory — use rmdir or rm({r:1})`);
            throw e2;
          }
          throw new Error(`rm: '${arg}' not found`);
        }
        throw e;
      }
    }
  }

  return VOID;
}
