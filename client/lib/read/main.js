import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
read — read a file's contents as a string

Usage:
  path.read()               read file at path, return string
  gimbal.read(path)            same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('read: need a file path');

  let target = prev;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      target = prev.p(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('read: unexpected argument');
    }
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(target._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const file = await vol.lookup(r.path);
    return file.text();
  });
}
