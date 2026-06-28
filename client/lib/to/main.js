import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
to — write pipeline content to a destination path

Usage:
  response.to(dest)         write Response body to dest (GimbalPath)
  string.to(dest)           write string directly to dest

Accepts a Response or string. Writes the full content to dest.`;

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export function invoke(gimbal, prev, ...args) {
  if (typeof prev !== 'string' && !(prev instanceof Response))
    throw new Error(`to: expected a string or Response, got ${prev?.constructor?.name ?? typeof prev}`);

  let dest = null;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && a instanceof GimbalPath) {
      dest = a;
    } else if (i === 0 && typeof a === 'string') {
      dest = gimbal.p(a);
    } else if (i === args.length - 1 && isPlainObject(a)) {
      opts = a;
    } else {
      throw new Error('to: unexpected argument — expected a GimbalPath destination');
    }
  }

  if (!dest) throw new Error('to: need a destination path (GimbalPath)');

  return new GimbalResult(async () => {
    let bytes;
    if (typeof prev === 'string') {
      bytes = new TextEncoder().encode(prev);
    } else {
      bytes = new Uint8Array(await prev.arrayBuffer());
    }
    const r = gimbal.resolvePath(dest._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const contentCID = await vol.put(bytes);
    const metaCID = await vol.mkfile(contentCID, bytes.byteLength);
    try {
      await vol.multiLink([{ path: r.path, addr: metaCID, assert: 2 }]);
    } catch (e) {
      try {
        await vol.multiLink([{ path: r.path, addr: metaCID, prevAddr: '', assert: 1 }]);
      } catch (e2) {
        throw new Error(`to: cannot write to '${r.path}'`);
      }
    }
  });
}
