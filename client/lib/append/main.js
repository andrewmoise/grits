import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export const help = `\
append — append content to the end of a file

Usage:
  path.append(content)        append string content to file at path
  gimbal.append(path, content)   same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('append: need a destination path');
  if (args.length > 1) throw new Error('append: too many arguments');
  const content = args[0];
  if (content === undefined) throw new Error('append: no content provided');
  if (typeof content !== 'string' && !(content instanceof Response) && !(content instanceof Uint8Array) && !(content instanceof ArrayBuffer))
    throw new Error('append: content must be string, Response, or binary data');
  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(prev._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);

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
