import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
read — read a file's contents as a string

Usage:
  path.read()               read file at path, return string
  gimbal.read(path)            same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return null;
}

export function invoke(gimbal, prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('read: need a file path');
  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(path._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const file = await vol.lookup(r.path);
    return file.text();
  });
}
