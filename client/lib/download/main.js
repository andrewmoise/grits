// lib/download/main.js
export const help = `\
download — fetch a URL and bring the response into the pipeline

Usage:
  download('https://example.com/data.json')
  download('https://example.com/data.json', { headers: { 'Authorization': 'Bearer ...' } })
  download('https://example.com/upload', { method: 'POST', body: '...' })

Output is a Response. Pipe to .to() to save, or let coercion handle text/JSON.
Error thrown on network failure; HTTP error status codes are passed through as-is.`;

import { isVoid } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('download: does not accept pipeline input — use as entry point only');

  const [url, init] = args;
  if (!url || typeof url !== 'string')
    throw new Error('download: URL string required as first argument');

  let response;
  try {
    response = await fetch(url, init);
  } catch (e) {
    throw new Error(`download: network error fetching ${url}: ${e.message}`);
  }

  return response;
}