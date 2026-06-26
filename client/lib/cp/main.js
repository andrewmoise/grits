import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export const help = `\
cp — copy a file to a new path

Usage:
  path.cp(dest)            copy to dest (GimbalPath or string)
  path.cp(dest, {f:1})     overwrite even if dest is a directory
  path.cp(dest, {i:1})     fail if dest exists at all
  gsh.cp(src, dest)        same (paths must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

function isPathNotFound(e) {
  const msg = e.message || '';
  return msg.includes('file does not exist') || msg.includes('is not a directory');
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
  if (!(src instanceof GimbalPath)) throw new Error('cp: need a source path');

  const dest = findDest(args, src._shell);
  if (!dest) throw new Error('cp: need a destination path');

  const opts = findOpts(args);
  const shell = src._shell;
  return new GimbalResult(async () => {
    const srcR = src._shell.resolvePath(src.abs());
    const destR = dest._shell.resolvePath(dest.abs());
    const srcVol = shell._vol(srcR.serverUrl, srcR.volume);
    const destVol = shell._vol(destR.serverUrl, destR.volume);

    const srcFile = await srcVol.lookup(srcR.path);
    const bytes = new Uint8Array(await (await srcFile.get()).arrayBuffer());
    const contentCID = await destVol.put(bytes);
    const metaCID = await destVol.mkfile(contentCID, bytes.byteLength);

    const srcName = srcR.path.split('/').at(-1);
    const candidates = opts.ff ? [destR.path] : [destR.path + '/' + srcName, destR.path];

    for (const path of candidates) {
      try {
        if (opts.i) {
          await destVol.multiLink([{ path, addr: metaCID, prevAddr: '', assert: ASSERT_PREV_MATCHES }]);
        } else if (opts.f || opts.ff) {
          await destVol.multiLink([{ path, addr: metaCID }]);
        } else {
          try { await destVol.multiLink([{ path, addr: metaCID, assert: ASSERT_IS_BLOB }]); }
          catch (e) {
            if (!(e instanceof AssertionError)) throw e;
            await destVol.multiLink([{ path, addr: metaCID, prevAddr: '', assert: ASSERT_PREV_MATCHES }]);
          }
        }
        return;
      } catch (e) {
        if (e instanceof AssertionError) {
          if (path === candidates[candidates.length - 1]) throw e;
        } else if (isPathNotFound(e)) {
          continue;
        } else {
          throw e;
        }
      }
    }
  });
}
