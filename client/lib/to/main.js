// lib/to/main.js
export const help = `\
to — write pipeline output to a path on the filesystem

Usage:
  from('input.txt').to('output.txt')    copy a file
  wget(url).to('cached.html')           save a response to a file

Input:  GritsFile or Response (required)
Output: GritsFile at the destination path`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [destPath] = positional;

  if (!destPath || typeof destPath !== 'string')
    throw new Error('to: destination path string required');

  const prev = await previous;
  if (isVoid(prev))
    throw new Error('to: requires pipeline input');

  const vol      = shell._currentVol();
  const resolved = shell.resolvePath(destPath).replace(/^\//, '');

  let data;
  if (prev instanceof GritsFile) {
    const buf = await prev.bytes();
    data = new Uint8Array(buf);
  } else if (prev instanceof Response) {
    const buf = await prev.arrayBuffer();
    data = new Uint8Array(buf);
  } else {
    throw new Error(`to: input must be a GritsFile or Response, got ${prev?.constructor?.name ?? typeof prev}`);
  }

  const contentCID = await vol.put(data);
  const metaCID    = await vol.mkfile(contentCID, data.byteLength);
  await vol.li(metaCID, resolved);
  return vol.lo(resolved);
}