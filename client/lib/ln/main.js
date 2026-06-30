import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError } from '../grits/GritsClient.js';

export const help = `\
ln — link a file into the filesystem (copy-on-write)

Usage:
  path.ln(dest)              link to dest (GimbalPath or string)
  path.ln(dest, {i:1})       fail if dest exists
  gimbal.ln(src, dest)       same (paths must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('ln: need a source path');

  let dest = null;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && a instanceof GimbalPath) {
      dest = a;
    } else if (i === 0 && typeof a === 'string') {
      dest = prev.relPath(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('ln: unexpected argument');
    }
  }

  if (!dest) throw new Error('ln: need a destination path');

  return new GimbalResult(async () => {
    const srcR = gimbal.resolvePath(prev.abs());
    const destR = gimbal.resolvePath(dest.abs());
    const srcVol = gimbal.grits.volume(gimbal._serverUrl, srcR.volumeName);
    const destVol = gimbal.grits.volume(gimbal._serverUrl, destR.volumeName);
    const srcFile = await srcVol.lookup(srcR.path);
    const srcName = srcR.path.split('/').at(-1);
    const candidates = opts.ff ? [destR.path] : [destR.path + '/' + srcName, destR.path];

    for (let i = 0; i < candidates.length; i++) {
      const path = candidates[i];
      if (opts.i) {
        try { await destVol.lookup(path); throw new Error('ln: destination exists'); }
        catch (e) { if (e.message === 'ln: destination exists') throw e; }
      }
      try {
        await destVol.multiLink([{ path, addr: srcFile.cid(), assert: 0 }]);
        return;
      } catch (e) {
        if (i < candidates.length - 1) continue;
        if (e instanceof AssertionError) throw new Error('ln: destination exists');
        throw e;
      }
    }
    throw new Error('ln: cannot resolve destination');
  });
}
