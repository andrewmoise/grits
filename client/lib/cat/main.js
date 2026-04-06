// lib/cat/main.js
export const help = `\
cat — concatenate files and pipeline input into a Response bytestream

Usage:
  cat('path/to/file')              read a single file as Response
  cat('file1', 'file2', ...)       concatenate multiple files
  <input>.cat()                    wrap pipeline input as Response
  <input>.cat('file1', ...)        prepend pipeline input, then files

Always returns a Response. For type-preserving single-file access use from().`;

import { isVoid, _isPlainObject, coerceToBytes } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'cat') {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const prev    = await previous;
  const hasPrev = !isVoid(prev);

  if (!hasPrev && positional.length === 0)
    throw new Error(`${cmd}: argument required`);

  for (const arg of positional)
    if (typeof arg !== 'string')
      throw new Error(`${cmd}: path arguments must be strings`);

  // Trivial single-source cases — preserve content-type if available.
  if (!hasPrev && positional.length === 1) {
    const { serverUrl, volume, path } = shell.resolvePath(positional[0]);
    const file = await shell._vol(serverUrl, volume).lookup(path);
    return file.get();
  }
  if (hasPrev && positional.length === 0) {
    const bytes = await coerceToBytes(prev, shell);
    const contentType = prev instanceof Response
      ? prev.headers.get('Content-Type')
      : null;
    const headers = contentType ? { 'Content-Type': contentType } : {};
    return new Response(bytes, { status: 200, headers });
  }

  // Multi-source — concatenate into a plain Response, no content-type.
  const chunks = [];

  if (hasPrev)
    chunks.push(await coerceToBytes(prev, shell));

  for (const pathArg of positional) {
    const { serverUrl, volume, path } = shell.resolvePath(pathArg);
    const file = await shell._vol(serverUrl, volume).lookup(path);
    chunks.push(new Uint8Array(await (await file.get()).arrayBuffer()));
  }

  const totalLength = chunks.reduce((n, c) => n + c.byteLength, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.byteLength;
  }

  return new Response(result, { status: 200 });
}