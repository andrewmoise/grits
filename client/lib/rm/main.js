import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_IS_BLOB, ASSERT_IS_TREE, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
rm — remove files or directories

Usage:
  path.rm()                   remove file, fail if directory
  path.rm({r:1})              remove file or directory
  path.rm({f:1})              ignore if missing, fail on directory
  gimbal.rm(path)                same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('rm: need a path');

  let target = prev;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      target = prev.p(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('rm: unexpected argument');
    }
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(target._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const p = r.path;

    if (opts.r && opts.f) {
      await vol.multiLink([{ path: p, addr: '', assert: 0 }]);
    } else if (opts.r) {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_TREE }]); }
      catch (e) {
        if (e instanceof AssertionError) await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]);
        else throw e;
      }
    } else if (opts.f) {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]); }
      catch (e) {
        if (e instanceof AssertionError) {
          try { await vol.multiLink([{ path: p, addr: '', prevAddr: '', assert: ASSERT_PREV_MATCHES }]); }
          catch (e2) {
            if (e2 instanceof AssertionError) throw new Error(`rm: is a directory`);
            throw e2;
          }
        } else throw e;
      }
    } else {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]); }
      catch (e) {
        if (e instanceof AssertionError) {
          try { await vol.multiLink([{ path: p, addr: '', prevAddr: '', assert: ASSERT_PREV_MATCHES }]); }
          catch (e2) {
            if (e2 instanceof AssertionError) throw new Error(`rm: is a directory`);
            throw e2;
          }
          throw new Error(`rm: not found`);
        }
        throw e;
      }
    }
  });
}
