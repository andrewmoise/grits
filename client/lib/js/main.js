import { GimbalResult } from '../gimbal/result.js';

export const help = `\
js — parse a JSON string into a JavaScript value

Usage:
  string.js()               parse prev as JSON

Accepts a string. Returns the parsed value.`;

export function invoke(gimbal, prev) {
  if (typeof prev !== 'string')
    throw new Error(`js: expected a JSON string, got ${prev?.constructor?.name ?? typeof prev}`);

  return new GimbalResult(() => {
    return JSON.parse(prev);
  });
}
