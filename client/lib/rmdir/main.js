import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_IS_TREE } from '../grits/GritsClient.js';

export const help = `\
rmdir — remove an empty directory

Usage:
  path.rmdir()          remove empty directory
  gimbal.rmdir(path)       same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return null;
}

export function invoke(gimbal, prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('rmdir: need a path');
  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(path._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    try { await vol.multiLink([{ path: r.path, addr: '', assert: ASSERT_IS_TREE }]); }
    catch (e) {
      if (e instanceof AssertionError) throw new Error(`rmdir: not found or not a directory`);
      throw e;
    }
  });
}
