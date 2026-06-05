import { VOID, isVoid } from '../gimbal/gsh.js';

export const help = `\
cd — change current directory

Usage:
  cd('path')              relative or absolute path
  cd('//volume/path')     different volume, same server
  cd('//host:vol/path')   different server and volume`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('cd: does not accept pipeline input');

  const [value = '/'] = args;
  if (typeof value !== 'string')
    throw new Error('cd: path must be a string');

  const { serverUrl, volume, path } = shell.resolvePath(value);
  const file = await shell._vol(serverUrl, volume).lookup(path);
  if (!file.isDir()) {
    throw new Error(`cd: not a directory: ${value}`);
  }

  previous._parent.serverUrl = serverUrl;
  previous._parent.volume    = volume;
  previous._parent.cwd       = path;

  return VOID;
}