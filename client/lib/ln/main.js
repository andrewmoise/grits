import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
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
  gimbal.ln(src, dest)          same (paths must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return args.find(a => a instanceof GimbalPath) || null;
}

function findDest(args, gimbal) {
  const p = args.find(a => a instanceof GimbalPath);
  if (p) return p;
  const str = args.find(a => typeof a === 'string');
  if (str && gimbal) {
    const res = gimbal.resolvePath(str);
    return gimbal.p('/' + res.path);
  }
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath)) || {};
}

export function invoke(gimbal, prev, ...args) {
  const src = resolvePath(prev, args);
  if (!(src instanceof GimbalPath)) throw new Error('ln: need a source path');

  const dest = findDest(args, gimbal);
  if (!dest) throw new Error('ln: need a destination path');

  const opts = findOpts(args);
  return new GimbalResult(async () => {
    const srcR = gimbal.resolvePath(src.abs());
    const destR = gimbal.resolvePath(dest.abs());
    const srcVol = gimbal.grits.volume(gimbal._serverUrl, srcR.volumeName);
    const destVol = gimbal.grits.volume(gimbal._serverUrl, destR.volumeName);
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
