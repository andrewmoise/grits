// lib/wget/main.js — fetch a URL, return a Response

export const help = `\
wget — fetch a URL, return a Response

Usage:
  wget('https://example.com')
  wget('https://example.com').grep('href')
  wget('https://api.example.com/data', { method: 'POST', body: '{}' })

Input:  void
Output: Response

Options (last arg):
  method  : string — HTTP method (default 'GET')
  headers : object — request headers
  body    : string — request body`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  // Peel off trailing options map if present
  const opts = (args.length > 0 && _isPlainObj(args[args.length - 1]))
    ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const [url] = positional;

  const prev = await previous;
  if (!isVoid(prev))
    console.warn('wget: ignoring piped input');

  if (!url || typeof url !== 'string')
    throw new Error('wget: first argument must be a URL string');

  const { method = 'GET', headers = {}, body } = opts;
  const response = await globalThis.fetch(url, { method, headers, body });

  if (!response.ok)
    throw new Error(`wget: ${url}: HTTP ${response.status} ${response.statusText}`);

  return response;
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}