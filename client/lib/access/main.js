import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
access — display ACL grants for a directory

Usage:
  path.access()    list ACL grants on path`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath))
    throw new Error('access: must be called on a path (use gimbal.p("/path").access())');
  if (args.length > 0)
    throw new Error('access: does not accept arguments');

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(prev.abs());
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';
    try {
      const file = await vol.lookup(jsonPath);
      return await file.json();
    } catch (e) {
      if (e.message.includes('not found')) return;
      throw e;
    }
  });
}
