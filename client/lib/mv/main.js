// lib/mv/main.js
export const help = `\
mv — move (rename) a file

Usage:
  mv('src', 'dest')          move, fail if dest exists
  mv('src', 'dest', {f:1})   overwrite dest`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';
import { resolveDestPaths, isPathNotFound } from '../ln/main.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('mv: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 2 ||
      typeof positional[0] !== 'string' ||
      typeof positional[1] !== 'string')
    throw new Error('mv: expected mv(src, dest)');

  const srcR  = shell.resolvePath(positional[0]);
  const destR = shell.resolvePath(positional[1]);

  const srcVol  = shell._vol(srcR.serverUrl, srcR.volume);
  const destVol = shell._vol(destR.serverUrl, destR.volume);

  const srcFile = await srcVol.lookup(srcR.path);
  const srcName = srcR.path.split('/').at(-1);

  const candidates = opts.ff
    ? [destR.path]
    : resolveDestPaths(destR, srcName);

  const isCrossVolume = srcR.serverUrl !== destR.serverUrl || srcR.volume !== destR.volume;

  if (isCrossVolume) {
    let destPath;
    let lastError;
    for (const path of candidates) {
      try {
        await destVol.multiLink([{
          path, addr: srcFile.cid(),
          prevAddr: (opts.f || opts.ff) ? undefined : '',
          assert:   (opts.f || opts.ff) ? 0 : ASSERT_PREV_MATCHES,
        }]);
        destPath = path;
        break;
      } catch (e) {
        if (e instanceof AssertionError)
          throw new Error(`mv: destination already exists — use {f:1} to overwrite`);
        if (isPathNotFound(e)) { lastError = e; continue; }
        throw e;
      }
    }
    if (!destPath) {
      if (destR.trailingSlash) throw new Error(`mv: destination is not a directory: '${positional[1]}'`);
      throw lastError || new Error('mv: cannot resolve destination');
    }
    try {
      await srcVol.multiLink([{
        path: srcR.path, addr: '',
        prevAddr: srcFile.cid(), assert: ASSERT_PREV_MATCHES,
      }]);
    } catch (_) {}
  } else {
    let lastError;
    for (const destPath of candidates) {
      try {
        await srcVol.multiLink([
          {
            path: destPath, addr: srcFile.cid(),
            prevAddr: (opts.f || opts.ff) ? undefined : '',
            assert:   (opts.f || opts.ff) ? 0 : ASSERT_PREV_MATCHES,
          },
          {
            path: srcR.path, addr: '',
            prevAddr: srcFile.cid(), assert: ASSERT_PREV_MATCHES,
          },
        ]);
        return VOID;
      } catch (e) {
        if (e instanceof AssertionError)
          throw new Error(`mv: destination already exists or source changed — use {f:1} to overwrite`);
        if (isPathNotFound(e)) { lastError = e; continue; }
        throw e;
      }
    }
    if (destR.trailingSlash) throw new Error(`mv: destination is not a directory: '${positional[1]}'`);
    throw lastError || new Error('mv: cannot resolve destination');
  }

  return VOID;
}
