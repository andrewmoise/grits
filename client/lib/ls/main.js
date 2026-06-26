import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { resolvePathArg } from '../gimbal/path-util.js';

export const help = `\
ls — list directory contents

Usage:
  path.ls()                list the directory at path
  gsh.ls(path)             same

Returns an array of GimbalPath objects for child entries.`;

export function invoke(prev, ...args) {
  const p = resolvePathArg(prev, args);
  if (!p) throw new Error('ls: need a directory path');

  const shell = p._shell;
  return new GimbalResult(async () => {
    const vol = shell._vol();
    const file = await vol.lookup(p.abs());
    if (!file.isDir()) throw new Error('ls: not a directory');
    const children = await file.children();
    return [...children.keys()].sort().map(name => new GimbalPath(p.abs().replace(/\/?$/, '/') + name, shell));
  });
}
