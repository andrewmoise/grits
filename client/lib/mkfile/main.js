// lib/mkfile/main.js — upload content, return a GritsFile

export const help = `\
mkfile — upload content to the blob store, return a GritsFile

Usage:
  mkfile('string content')       from a string argument
  cat('a.txt').mkfile()          from stdin (Response, bytes, …)
  echo('hello').mkfile()         same

Input:  string | ArrayBuffer | Uint8Array | Response  (arg OR stdin)
Output: GritsFile

mkfile() is the on-ramp for raw content into the type cascade.
Chain .li('path') after it to link the result into the namespace.`;

import { isVoid, coerceToBytes } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const [contentArg] = args;

  let source;
  if (contentArg !== undefined) {
    source = contentArg;
  } else {
    source = await previous;
    if (isVoid(source))
      throw new Error('mkfile: no content — pass content as an argument or pipe it in');
  }

  let bytes;
  if (typeof source === 'string') {
    bytes = new TextEncoder().encode(source);
  } else {
    bytes = await coerceToBytes(source, shell);
  }

  const vol        = shell._currentVol();
  const contentCID = await vol.put(bytes);
  return vol.mkfile(contentCID, bytes.byteLength);
}