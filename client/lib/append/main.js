// lib/append/main.js
export const help = `\
append — append pipeline input to the end of a file

Usage:
  <input>.append('path')      append input to 'path', creating if missing

Like >> in shell: reads the current file content, then streams the pipeline
input after it, and overwrites the file with the combined result.`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('append: expected exactly one destination path argument');

  const prev = await previous;
  if (isVoid(prev))
    throw new Error('append: requires pipeline input');
  if (!(prev instanceof Response))
    throw new Error('append: pipeline input must be a Response');

  const destR   = shell.resolvePath(positional[0]);
  const destVol = shell._vol(destR.serverUrl, destR.volume);

  let existingBytes = new Uint8Array(0);
  try {
    const file = await destVol.lookup(destR.path);
    if (file.isDir())
      throw new Error(`append: destination is a directory: '${positional[0]}'`);
    existingBytes = new Uint8Array(await file.bytes());
  } catch (e) {
    if (e.message.startsWith('append:'))
      throw e;
    if (!e.message.includes('not found'))
      throw e;
  }

  const inputBytes = new Uint8Array(await prev.arrayBuffer());
  const combined   = new Uint8Array(existingBytes.byteLength + inputBytes.byteLength);
  combined.set(existingBytes, 0);
  combined.set(inputBytes, existingBytes.byteLength);

  const contentCID = await destVol.put(combined);
  const metaCID    = await destVol.mkfile(contentCID, combined.byteLength);

  try {
    await destVol.multiLink([{
      path:   destR.path,
      addr:   metaCID,
      assert: ASSERT_IS_BLOB,
    }]);
    return VOID;
  } catch (e) {
    if (!(e instanceof AssertionError)) throw e;
  }

  try {
    await destVol.multiLink([{
      path:     destR.path,
      addr:     metaCID,
      prevAddr: '',
      assert:   ASSERT_PREV_MATCHES,
    }]);
    return VOID;
  } catch (e) {
    if (!(e instanceof AssertionError)) throw e;
    throw new Error(`append: destination is a directory: '${positional[0]}'`);
  }
}
