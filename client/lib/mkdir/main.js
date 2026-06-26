import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
mkdir — create a directory

Usage:
  path.mkdir()                create, fail if exists
  path.mkdir({p:1})           create parent directories as needed
  path.mkdir({f:1})           silently succeed if already a directory
  gsh.mkdir(path)             same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) || {};
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('mkdir: need a path');

  const opts = findOpts(args);
  const shell = path._shell;

  return new GimbalResult(async () => {
    const r = shell.resolvePath(path.abs());
    const vol = shell._vol(r.serverUrl, r.volume);

    if (opts.p) {
      const parts = r.path.split('/').filter(Boolean);
      let cur = '';
      for (const part of parts) {
        cur += '/' + part;
        const metaCID = await vol.mkdir({});
        try { await vol.multiLink([{ path: cur, addr: metaCID, prevAddr: '', assert: ASSERT_PREV_MATCHES }]); continue; }
        catch (e) { if (!(e instanceof AssertionError)) throw e; }
        if (opts.f) {
          try { await vol.multiLink([{ path: cur, addr: metaCID, assert: 2 }]); continue; }
          catch (e) { if (!(e instanceof AssertionError)) throw e; }
        }
        const existing = await vol.lookup(cur).catch(() => null);
        if (existing && !existing.isDir()) throw new Error(`mkdir: not a directory: '${cur}'`);
      }
      return;
    }

    const metaCID = await vol.mkdir({});
    try {
      await vol.multiLink([{ path: r.path, addr: metaCID, prevAddr: opts.f ? undefined : '', assert: opts.f ? 0 : ASSERT_PREV_MATCHES }]);
    } catch (e) {
      if (e instanceof AssertionError) throw new Error(`mkdir: already exists`);
      throw e;
    }
  });
}
