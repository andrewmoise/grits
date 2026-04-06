// lib/to/main.js
export const help = `\
to — write pipeline input to a path

Usage:
  <input>.to('path')          write, overwrite if file, fail if directory
  <input>.to('path', {f:1})   overwrite even if dest is a directory
  <input>.to('path', {i:1})   fail if dest exists at all

Unlike cp/ln, to() requires the full destination path including filename.
It does not remap into a directory automatically.`;

import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('to: expected exactly one destination path argument');

  const prev = await previous;
  if (isVoid(prev))
    throw new Error('to: requires pipeline input');
  if (!(prev instanceof Response))
    throw new Error('to: pipeline input must be a Response');

  const destR   = shell.resolvePath(positional[0]);
  const destVol = shell._vol(destR.serverUrl, destR.volume);

  const bytes      = new Uint8Array(await prev.arrayBuffer());
  const contentCID = await destVol.put(bytes);
  const metaCID    = await destVol.mkfile(contentCID, bytes.byteLength);

  if (opts.f) {
    await destVol.multiLink([{ path: destR.path, addr: metaCID }]);
    return VOID;
  }

  if (opts.i) {
    try {
      await destVol.multiLink([{
        path:     destR.path,
        addr:     metaCID,
        prevAddr: '',
        assert:   ASSERT_PREV_MATCHES,
      }]);
    } catch (e) {
      if (e instanceof AssertionError)
        throw new Error(`to: destination already exists: '${positional[0]}'`);
      throw e;
    }
    return VOID;
  }

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
    throw new Error(`to: destination is a directory: '${positional[0]}' — use {f:1} to overwrite`);
  }
}