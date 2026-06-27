import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
mkdir — create a directory

Usage:
  path.mkdir()                create, fail if exists
  path.mkdir({p:1})           create parent directories as needed
  path.mkdir({f:1})           silently succeed if already a directory
  gimbal.mkdir(path)             same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('mkdir: need a path');

  let target = prev;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      target = prev.p(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('mkdir: unexpected argument');
    }
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(target._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);

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
