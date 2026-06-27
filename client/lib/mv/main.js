import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
mv — move (rename) a file system entry

Usage:
  path.mv(dest)            move to dest (GimbalPath or string)
  gimbal.mv(src, dest)        same (paths must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return null;
}

function findDest(args, gimbal) {
  const p = args.find(a => a instanceof GimbalPath);
  if (p) return p;
  const str = args.find(a => typeof a === 'string');
  if (str && gimbal) return gimbal.p(str);
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) || {};
}

export function invoke(gimbal, prev, ...args) {
  const src = resolvePath(prev, args);
  if (!(src instanceof GimbalPath)) throw new Error('mv: need a source path');

  const dest = findDest(args, gimbal);
  if (!dest) throw new Error('mv: need a destination path');

  const opts = findOpts(args);
  return new GimbalResult(async () => {
    const srcR = gimbal.resolvePath(src.abs());
    const destR = gimbal.resolvePath(dest.abs());
    const srcVol = gimbal.grits.volume(gimbal._serverUrl, srcR.volumeName);
    const destVol = gimbal.grits.volume(gimbal._serverUrl, destR.volumeName);

    const srcFile = await srcVol.lookup(srcR.path);
    const srcName = srcR.path.split('/').at(-1);
    const candidates = opts.ff ? [destR.path] : [destR.path + '/' + srcName, destR.path];
    const isCross = srcR.volumeName !== destR.volumeName;

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
