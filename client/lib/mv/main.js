import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
mv — move (rename) a file system entry

Usage:
  path.mv(dest)            move to dest (GimbalPath)
  gsh.mv('/src', dest)     same`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    const p = args.find(a => a instanceof GimbalPath);
    if (p) return p;
    const str = args.find(a => typeof a === 'string');
    if (str) return new GimbalPath('/' + prev.resolvePath(str).path, prev);
  }
  return null;
}

function findDest(args) {
  const path = args.find(a => a instanceof GimbalPath);
  if (path) return path;
  const result = args.find(a => a instanceof GimbalResult);
  if (result) return result;
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) || {};
}

export function invoke(prev, ...args) {
  const src = resolvePath(prev, args);
  if (!(src instanceof GimbalPath)) throw new Error('mv: need a source path');

  const dest = findDest(args);
  if (!dest) throw new Error('mv: need a destination path');

  if (dest instanceof GimbalResult) {
    return new GimbalResult(async () => {
      const resolved = await dest;
      const remaining = args.filter(a => a !== dest);
      return invoke(prev, resolved, ...remaining);
    });
  }

  const opts = findOpts(args);
  const shell = src._shell;
  return new GimbalResult(async () => {
    const srcR = src._shell.resolvePath(src.abs());
    const destR = dest._shell.resolvePath(dest.abs());
    const srcVol = shell._vol(srcR.serverUrl, srcR.volume);
    const destVol = shell._vol(destR.serverUrl, destR.volume);

    const srcFile = await srcVol.lookup(srcR.path);
    const srcName = srcR.path.split('/').at(-1);
    const candidates = opts.ff ? [destR.path] : [destR.path + '/' + srcName, destR.path];
    const isCross = srcR.serverUrl !== destR.serverUrl || srcR.volume !== destR.volume;

    if (isCross) {
      const bytes = new Uint8Array(await (await srcFile.get()).arrayBuffer());
      const contentCID = await destVol.put(bytes);
      const metaCID = await destVol.mkfile(contentCID, bytes.byteLength);
      for (const path of candidates) {
        try {
          await destVol.multiLink([{ path, addr: metaCID, prevAddr: opts.i ? '' : undefined, assert: opts.i ? ASSERT_PREV_MATCHES : 0 }]);
        } catch (e) {
          if (e instanceof AssertionError) throw new Error(`mv: destination exists`);
          continue;
        }
        await srcVol.multiLink([{ path: srcR.path, addr: '' }]);
        return;
      }
      throw new Error(`mv: cannot resolve destination`);
    }

    for (const path of candidates) {
      try {
        const prevAddr = opts.i ? '' : undefined;
        const assert = opts.i ? ASSERT_PREV_MATCHES : (opts.f || opts.ff ? 0 : ASSERT_PREV_MATCHES);
        await srcVol.multiLink([
          { path, addr: srcFile.cid(), prevAddr, assert },
          { path: srcR.path, addr: '', prevAddr: srcFile.cid(), assert: ASSERT_PREV_MATCHES },
        ]);
        return;
      } catch (e) {
        if (e instanceof AssertionError) throw new Error(`mv: destination exists`);
      }
    }
    throw new Error(`mv: cannot resolve destination`);
  });
}
