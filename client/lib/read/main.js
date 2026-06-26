import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
read — read a file's contents as a string

Usage:
  path.read()               read file at path, return string
  gsh.read(path)            same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('read: need a file path');

  const shell = path._shell;
  return new GimbalResult(async () => {
    const vol = shell._vol();
    const file = await vol.lookup(path.abs());
    return file.text();
  });
}
