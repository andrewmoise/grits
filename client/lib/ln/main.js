// lib/ln/main.js
export const help = `\
ln — link a file into the filesystem

Usage:
  ln('src', 'dest')          link into dest if dir, overwrite if file
  ln('src', 'dest/')         dest must be a directory, place inside it
  ln('src', 'dest', {f:1})   overwrite even if dest is a directory
  ln('src', 'dest', {i:1})   fail if dest exists at all

ln() only moves pointers — it never uploads content.
Use to() to write a bytestream to a path.`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export function resolveDestPaths(destR, srcName) {
  if (destR.trailingSlash) {
    return [`${destR.path}/${srcName}`];
  }
  return [`${destR.path}/${srcName}`, destR.path];
}

export function isPathNotFound(e) {
  const msg = e.message || '';
  return msg.includes('file does not exist') || msg.includes('is not a directory');
}

export async function invoke(shell, previous, args, cmd = 'ln') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 2 ||
      typeof positional[0] !== 'string' ||
      typeof positional[1] !== 'string')
    throw new Error(`${cmd}: expected ${cmd}(src, dest)`);

  const [srcArg, destArg] = positional;
  const srcR  = shell.resolvePath(srcArg);
  const destR = shell.resolvePath(destArg);

  const srcVol  = shell._vol(srcR.serverUrl, srcR.volume);
  const destVol = shell._vol(destR.serverUrl, destR.volume);

  const srcFile = await srcVol.lookup(srcR.path);
  const srcName = srcR.path.split('/').at(-1);
  const candidates = opts.ff
    ? [destR.path]
    : resolveDestPaths(destR, srcName);

  if (opts.f || opts.ff) {
    let lastError;
    for (const path of candidates) {
      try {
        await destVol.multiLink([{ path, addr: srcFile.cid() }]);
        return VOID;
      } catch (e) {
        if (e instanceof AssertionError) throw e;
        if (isPathNotFound(e)) { lastError = e; continue; }
        throw e;
      }
    }
    if (destR.trailingSlash) throw new Error(`${cmd}: destination is not a directory: '${destArg}'`);
    throw lastError || new Error(`${cmd}: cannot resolve destination: '${destArg}'`);
  }

  if (opts.i) {
    let lastError;
    for (const path of candidates) {
      try {
        await destVol.multiLink([{
          path, addr: srcFile.cid(),
          prevAddr: '', assert: ASSERT_PREV_MATCHES,
        }]);
        return VOID;
      } catch (e) {
        if (e instanceof AssertionError)
          throw new Error(`${cmd}: destination already exists: '${destArg}'`);
        if (isPathNotFound(e)) { lastError = e; continue; }
        throw e;
      }
    }
    if (destR.trailingSlash) throw new Error(`${cmd}: destination is not a directory: '${destArg}'`);
    throw lastError || new Error(`${cmd}: cannot resolve destination: '${destArg}'`);
  }

  let lastError;
  for (const path of candidates) {
    try {
      await destVol.multiLink([{
        path, addr: srcFile.cid(), assert: ASSERT_IS_BLOB,
      }]);
      return VOID;
    } catch (e) {
      if (!(e instanceof AssertionError)) {
        if (isPathNotFound(e)) { lastError = e; continue; }
        throw e;
      }
    }
    try {
      await destVol.multiLink([{
        path, addr: srcFile.cid(),
        prevAddr: '', assert: ASSERT_PREV_MATCHES,
      }]);
      return VOID;
    } catch (e) {
      if (e instanceof AssertionError)
        throw new Error(`${cmd}: destination is a directory: '${destArg}' — use {f:1} to overwrite`);
      throw e;
    }
  }
  if (destR.trailingSlash) throw new Error(`${cmd}: destination is not a directory: '${destArg}'`);
  throw lastError || new Error(`${cmd}: cannot resolve destination: '${destArg}'`);
}
