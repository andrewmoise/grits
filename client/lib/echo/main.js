// lib/echo/main.js — emit a string as a Response

export const help = `\
echo — emit a string as a Response

Usage:
  echo('hello world')
  echo('hello world').grep('world')

Input:  void
Output: Response`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const [text] = args;

  const prev = await previous;
  if (!isVoid(prev))
    console.warn('echo: ignoring piped input');

  if (text === undefined)
    throw new Error('echo: first argument must be a string');

  const bytes = new TextEncoder().encode(String(text));
  return new Response(bytes, {
    status:  200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
}