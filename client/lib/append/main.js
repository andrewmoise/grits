import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export const help = `\
append — append content to the end of a file

Usage:
  path.append(content)        append string content to file at path
  gsh.append(path, content)   same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('append: need a destination path');

  const nonPathArgs = args.filter(a => !(a instanceof GimbalPath) && !(a instanceof GimbalResult));
  const content = nonPathArgs[0];
  if (content === undefined) throw new Error('append: no content provided');

  const shell = path._shell;
  return new GimbalResult(async () => {
    const r = shell.resolvePath(path.abs());
    const vol = shell._vol(r.serverUrl, r.volume);

    let existingBytes = new Uint8Array(0);
    try {
      const file = await vol.lookup(r.path);
      if (file.isDir()) throw new Error(`append: destination is a directory`);
      existingBytes = new Uint8Array(await file.bytes());
    } catch (e) {
      if (e.message.startsWith('append:')) throw e;
      if (!e.message.includes('not found')) throw e;
    }

    const inputBytes = typeof content === 'string'
      ? new TextEncoder().encode(content)
      : content instanceof Uint8Array ? content
      : content instanceof ArrayBuffer ? new Uint8Array(content)
      : new TextEncoder().encode(String(content));

    const combined = new Uint8Array(existingBytes.length + inputBytes.length);
    combined.set(existingBytes, 0);
    combined.set(inputBytes, existingBytes.length);

    const contentCID = await vol.put(combined);
    const metaCID = await vol.mkfile(contentCID, combined.length);

    try { await vol.multiLink([{ path: r.path, addr: metaCID, assert: ASSERT_IS_BLOB }]); }
    catch (e) {
      if (!(e instanceof AssertionError)) throw e;
      try { await vol.multiLink([{ path: r.path, addr: metaCID, prevAddr: '', assert: ASSERT_PREV_MATCHES }]); }
      catch (e2) {
        if (e2 instanceof AssertionError) throw new Error(`append: destination is a directory`);
        throw e2;
      }
    }
  });
}
