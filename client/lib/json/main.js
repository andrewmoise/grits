import { GimbalResult } from '../gimbal/result.js';

export const help = `\
json — serialize a value to a JSON string

Usage:
  value.json()              encode prev as JSON string

Accepts any value. Returns a JSON string.`;

export function invoke(gimbal, prev) {
  return new GimbalResult(() => {
    return JSON.stringify(prev);
  });
}
