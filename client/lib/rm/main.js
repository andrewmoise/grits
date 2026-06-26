import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_IS_BLOB, ASSERT_IS_TREE, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export const help = `\
rm — remove files or directories

Usage:
  path.rm()                   remove file, fail if directory
  path.rm({r:1})              remove file or directory
  path.rm({f:1})              ignore if missing, fail on directory
  gsh.rm(path)                same (path must be GimbalPath)`;

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
  if (!(path instanceof GimbalPath)) throw new Error('rm: need a path');

  const opts = findOpts(args);
  const shell = path._shell;

  return new GimbalResult(async () => {
    const r = shell.resolvePath(path.abs());
    const vol = shell._vol(r.serverUrl, r.volume);
    const p = r.path;

    if (opts.r && opts.f) {
      await vol.multiLink([{ path: p, addr: '', assert: 0 }]);
    } else if (opts.r) {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_TREE }]); }
      catch (e) {
        if (e instanceof AssertionError) await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]);
        else throw e;
      }
    } else if (opts.f) {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]); }
      catch (e) {
        if (e instanceof AssertionError) {
          try { await vol.multiLink([{ path: p, addr: '', prevAddr: '', assert: ASSERT_PREV_MATCHES }]); }
          catch (e2) {
            if (e2 instanceof AssertionError) throw new Error(`rm: is a directory`);
            throw e2;
          }
        } else throw e;
      }
    } else {
      try { await vol.multiLink([{ path: p, addr: '', assert: ASSERT_IS_BLOB }]); }
      catch (e) {
        if (e instanceof AssertionError) {
          try { await vol.multiLink([{ path: p, addr: '', prevAddr: '', assert: ASSERT_PREV_MATCHES }]); }
          catch (e2) {
            if (e2 instanceof AssertionError) throw new Error(`rm: is a directory`);
            throw e2;
          }
          throw new Error(`rm: not found`);
        }
        throw e;
      }
    }
  });
}
