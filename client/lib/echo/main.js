// lib/echo/main.js — emit a string as a Response

export const help = `\
echo — emit a string as a Response, optionally JSON-encoded

Usage:
  echo('hello world')           ← "hello world\\n" (default adds newline)
  echo('hello world', {n:1})    ← "hello world"   (no trailing newline)
  echo('hello world', {j:1})    ← '"hello world"\\n'  (JSON-encoded + newline)
  echo('hello world', {j:1, n:1}) ← '"hello world"'   (JSON-encoded only)

Input:  void
Output: Response`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const prev = await previous;
  if (!isVoid(prev))
    console.warn('echo: ignoring piped input');

  if (positional.length !== 1)
    throw new Error('echo: first argument must be a string');

  let text = String(positional[0]);
  if (opts.j)
    text = JSON.stringify(text);
  if (!opts.n)
    text += '\n';

  const bytes = new TextEncoder().encode(text);
  return new Response(bytes, {
    status:  200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
}