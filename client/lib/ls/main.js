import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
ls — list directory contents

Usage:
  path.ls()                list the directory at path

Returns an array of GimbalPath objects for child entries.`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('ls: need a directory path');
  if (args.length > 0) throw new Error('ls: does not accept arguments');
  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(prev._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const file = await vol.lookup(r.path);
    if (!file.isDir()) throw new Error('ls: not a directory');
    const children = await file.children();
    return [...children.keys()].sort().map(name => gimbal.p(r.path.replace(/\/?$/, '/') + name));
  });
}
