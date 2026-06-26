import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_IS_TREE } from '../grits/GritsClient.js';

export const help = `\
rmdir — remove an empty directory

Usage:
  path.rmdir()          remove empty directory
  gsh.rmdir('/path')    same`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    const p = args.find(a => a instanceof GimbalPath);
    if (p) return p;
    const str = args.find(a => typeof a === 'string');
    if (str) return new GimbalPath('/' + prev.resolvePath(str).path, prev);
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('rmdir: need a path');

  const shell = path._shell;
  return new GimbalResult(async () => {
    const r = shell.resolvePath(path.abs());
    const vol = shell._vol(r.serverUrl, r.volume);
    try { await vol.multiLink([{ path: r.path, addr: '', assert: ASSERT_IS_TREE }]); }
    catch (e) {
      if (e instanceof AssertionError) throw new Error(`rmdir: not found or not a directory`);
      throw e;
    }
  });
}
