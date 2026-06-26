import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
write — write content to a file path

Usage:
  path.w(content)            write string content to path
  gsh.write('/path', str)    same`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    const p = args.find(a => a instanceof GimbalPath);
    if (p) return p;
    const str = args.find(a => typeof a === 'string' && a.startsWith('/'));
    if (str) return new GimbalPath(str, prev);
    return null;
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('write: need a destination path');

  // content is the argument after the path
  const nonPathArgs = args.filter(a => !(a instanceof GimbalPath));
  const content = nonPathArgs[0];
  if (content === undefined) throw new Error('write: no content provided');

  const shell = path._shell;
  return new GimbalResult(async () => {
    const vol = shell._vol();

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
      await vol.multiLink([{ path: path.abs(), addr: metaCID, assert: 2 }]);
    } catch (e) {
      try {
        await vol.multiLink([{ path: path.abs(), addr: metaCID, prevAddr: '', assert: 1 }]);
      } catch (e2) {
        throw new Error(`write: cannot write to '${path.abs()}'`);
      }
    }
  });
}
