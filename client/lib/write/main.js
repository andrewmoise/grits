import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
write — write content to a file path

Usage:
  path.w(content)            write string content to path
  gimbal.write(path, content)   same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return null;
}

export function invoke(gimbal, prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('write: need a destination path');

  const nonPathArgs = args.filter(a => !(a instanceof GimbalPath) && !(a instanceof GimbalResult));
  const content = nonPathArgs[0];
  if (content === undefined) throw new Error('write: no content provided');
  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(path._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);

    let bytes;
    if (typeof content === 'string') {
      bytes = new TextEncoder().encode(content);
    } else if (content instanceof Response) {
      bytes = new Uint8Array(await content.arrayBuffer());
    } else if (content instanceof Uint8Array) {
      bytes = content;
    } else if (content instanceof ArrayBuffer) {
      bytes = new Uint8Array(content);
    } else {
      bytes = new TextEncoder().encode(String(content));
    }

    const contentCID = await vol.put(bytes);
    const metaCID = await vol.mkfile(contentCID, bytes.byteLength);

    try {
      await vol.multiLink([{ path: r.path, addr: metaCID, assert: 2 }]);
    } catch (e) {
      try {
        await vol.multiLink([{ path: r.path, addr: metaCID, prevAddr: '', assert: 1 }]);
      } catch (e2) {
        throw new Error(`write: cannot write to '${r.path}'`);
      }
    }
  });
}
