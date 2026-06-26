import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
read — read a file's contents as a string

Usage:
  path.read()               read file at path, return string
  gsh.read('/path')         same`;

export function invoke(prev, ...args) {
  let path, shell;
  if (prev instanceof GimbalPath) {
    path = prev;
    shell = path._shell;
  } else if (prev instanceof GimbalShell) {
    shell = prev;
    path = args.find(a => a instanceof GimbalPath);
    if (!path) {
      const str = args.find(a => typeof a === 'string');
      if (str) path = new GimbalPath('/' + shell.resolvePath(str).path, shell);
    }
  } else if (prev instanceof GimbalResult) {
    return new GimbalResult(async () => {
      const resolved = await prev;
      return invoke(resolved, ...args);
    });
  }
  if (!(path instanceof GimbalPath)) throw new Error('read: need a file path');

  return new GimbalResult(async () => {
    const vol = shell._vol();
    const file = await vol.lookup(path.abs());
    return file.text();
  });
}
