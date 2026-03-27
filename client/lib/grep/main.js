// lib/grep/main.js — filter lines matching a pattern

export const help = `\
grep — filter lines matching a pattern

Usage:
  <input>.grep('pattern')
  <input>.grep('pattern', { ignoreCase: true, invert: true, fixed: true })
  grep('pattern', 'path/to/file')
  grep('pattern', 'path/to/file', { ignoreCase: true })

Input:  anything coercible to text (Response, GritsFile, string, …)
Output: string (matched lines joined by newline)

Options:
  invert     : boolean — return non-matching lines (like grep -v)
  ignoreCase : boolean — case-insensitive match (like grep -i)
  fixed      : boolean — treat pattern as a literal string, not a regex`;

import { isVoid, coerceToText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  // Peel off trailing options map if present
  const opts = (args.length > 0 && _isPlainObj(args[args.length - 1]))
    ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  const [pattern, pathArg] = positional;

  if (pattern === undefined || pattern === null)
    throw new Error('grep: first argument must be a pattern string or RegExp');

  // Source: explicit path arg, or stdin
  let source;
  if (pathArg !== undefined) {
    source = pathArg;
  } else {
    source = await previous;
    if (isVoid(source))
      throw new Error('grep: no input — pipe something in, or pass a path as the second argument');
  }

  const text = await coerceToText(source, shell);

  let re;
  if (pattern instanceof RegExp) {
    re = pattern;
  } else {
    const flags = opts.ignoreCase ? 'i' : '';
    if (opts.fixed) {
      const escaped = String(pattern).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      re = new RegExp(escaped, flags);
    } else {
      re = new RegExp(String(pattern), flags);
    }
  }

  const lines   = text.split('\n');
  const matched = opts.invert
    ? lines.filter(l => !re.test(l))
    : lines.filter(l =>  re.test(l));

  return matched.join('\n');
}

function _isPlainObj(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}