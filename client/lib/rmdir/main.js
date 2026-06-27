import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_IS_TREE } from '../grits/GritsClient.js';

export const help = `\
rmdir — remove an empty directory

Usage:
  path.rmdir()          remove empty directory
  gimbal.rmdir(path)       same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('rmdir: need a path');

  let target = prev;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      target = prev.p(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('rmdir: unexpected argument');
    }
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(target._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    try { await vol.multiLink([{ path: r.path, addr: '', assert: ASSERT_IS_TREE }]); }
    catch (e) {
      if (e instanceof AssertionError) throw new Error(`rmdir: not found or not a directory`);
      throw e;
    }
  });
}
