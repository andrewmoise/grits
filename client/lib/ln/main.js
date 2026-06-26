import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export function resolveDestPaths(destR, srcName) {
  if (destR.trailingSlash) return [`${destR.path}/${srcName}`];
  return [`${destR.path}/${srcName}`, destR.path];
}

export function isPathNotFound(e) {
  const msg = e.message || '';
  return msg.includes('file does not exist') || msg.includes('is not a directory');
}

export const help = `\
ln — link a file into the filesystem (copy-on-write)

Usage:
  path.ln(dest)              link to dest (GimbalPath or string)
  path.ln(dest, {ff:1})      overwrite dest
  gsh.ln(src, dest)          same (paths must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

function findDest(args, shell) {
  const p = args.find(a => a instanceof GimbalPath);
  if (p) return p;
  const str = args.find(a => typeof a === 'string');
  if (str && shell) {
    const res = shell.resolvePath(str);
    return new GimbalPath('/' + res.path, shell);
  }
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath)) || {};
}

export function invoke(prev, ...args) {
  const src = resolvePath(prev, args);
  if (!(src instanceof GimbalPath)) throw new Error('ln: need a source path');

  const dest = findDest(args, src._shell);
  if (!dest) throw new Error('ln: need a destination path');

  const opts = findOpts(args);
  const shell = src._shell;
  return new GimbalResult(async () => {
    const srcR = src._shell.resolvePath(src.abs());
    const destR = dest._shell.resolvePath(dest.abs());
    const srcVol = shell._vol(srcR.serverUrl, srcR.volume);
    const destVol = shell._vol(destR.serverUrl, destR.volume);
    const srcFile = await srcVol.lookup(srcR.path);
    const srcName = srcR.path.split('/').at(-1);
    const candidates = opts.ff ? [destR.path] : resolveDestPaths(destR, srcName);

    for (const path of candidates) {
      try {
        if (opts.f || opts.ff) {
          await destVol.multiLink([{ path, addr: srcFile.cid() }]);
        } else if (opts.i) {
          await destVol.multiLink([{ path, addr: srcFile.cid(), prevAddr: '', assert: ASSERT_PREV_MATCHES }]);
        } else {
          try {
            await destVol.multiLink([{ path, addr: srcFile.cid(), prevAddr: '', assert: ASSERT_PREV_MATCHES }]);
          } catch (e) {
            if (!(e instanceof AssertionError)) throw e;
            await destVol.multiLink([{ path, addr: srcFile.cid(), assert: ASSERT_IS_BLOB }]);
          }
        }
        return;
      } catch (e) {
        if (e instanceof AssertionError) throw new Error(`ln: destination exists`);
        if (isPathNotFound(e)) continue;
        throw e;
      }
    }
    throw new Error(`ln: cannot resolve destination`);
  });
}
