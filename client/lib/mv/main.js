// lib/mv/main.js
export const help = `\
mv — move (rename) a file

Usage:
  mv('src', 'dest')          move, fail if dest exists
  mv('src', 'dest', {f:1})   overwrite dest`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';
import { resolveDestPath } from '../ln/main.js';

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

  const destPath = opts.ff
    ? destR.path
    : await resolveDestPath(destVol, destR, srcName, 'mv');

  const isCrossVolume = srcR.serverUrl !== destR.serverUrl || srcR.volume !== destR.volume;

  if (isCrossVolume) {
    try {
      await destVol.multiLink([{
        path:     destPath,
        addr:     srcFile.cid(),
        prevAddr: (opts.f || opts.ff) ? undefined : '',
        assert:   (opts.f || opts.ff) ? 0 : ASSERT_PREV_MATCHES,
      }]);
    } catch (e) {
      if (e instanceof AssertionError)
        throw new Error(`mv: destination already exists — use {f:1} to overwrite`);
      throw e;
    }
    // Best-effort unlink of source — if this fails, data is safe at dest.
    try {
      await srcVol.multiLink([{
        path:     srcR.path,
        addr:     '',
        prevAddr: srcFile.cid(),
        assert:   ASSERT_PREV_MATCHES,
      }]);
    } catch (_) {}
  } else {
    try {
      await srcVol.multiLink([
        {
          path:     destPath,
          addr:     srcFile.cid(),
          prevAddr: (opts.f || opts.ff) ? undefined : '',
          assert:   (opts.f || opts.ff) ? 0 : ASSERT_PREV_MATCHES,
        },
        {
          path:     srcR.path,
          addr:     '',
          prevAddr: srcFile.cid(),
          assert:   ASSERT_PREV_MATCHES,
        },
      ]);
    } catch (e) {
      if (e instanceof AssertionError)
        throw new Error(`mv: destination already exists or source changed — use {f:1} to overwrite`);
      throw e;
    }
  }

  return VOID;
}
