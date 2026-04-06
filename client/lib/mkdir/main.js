// lib/mkdir/main.js
export const help = `\
mkdir — create a directory

Usage:
  mkdir('path')          create, fail if already exists
  mkdir('path', {f:1})   create or silently succeed if already a directory`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('mkdir: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length-1]) ? args[args.length-1] : {};
  const positional = opts === args[args.length-1] ? args.slice(0,-1) : [...args];

  if (positional.length !== 1 || typeof positional[0] !== 'string')
    throw new Error('mkdir: expected mkdir(path)');

  const r    = shell.resolvePath(positional[0]);
  const vol  = shell._vol(r.serverUrl, r.volume);
  const metaCID = await vol.mkdir({});

  try {
    await vol.multiLink([{
      path:     r.path,
      addr:     metaCID,
      prevAddr: opts.f ? undefined : '',
      assert:   opts.f ? 0 : ASSERT_PREV_MATCHES,
    }]);
  } catch(e) {
    if (e instanceof AssertionError)
      throw new Error(`mkdir: already exists: '${positional[0]}'`);
    throw e;
  }

  return VOID;
}