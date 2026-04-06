// lib/grep/main.js
export const help = `\
grep — filter lines matching a pattern

Usage:
  <input>.grep('pattern')                    filter pipeline input
  grep('pattern', 'path1', 'path2', ...)     filter named files

Options:
  {invert:true}      return non-matching lines
  {ignoreCase:true}  case-insensitive match
  {fixed:true}       treat pattern as literal string`;

import { isVoid, _isPlainObject, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pattern, ...paths] = positional;

  if (pattern === undefined)
    throw new Error('grep: pattern required');

  const prev     = await previous;
  const hasInput = !isVoid(prev);
  const hasPaths = paths.length > 0;

  if (hasInput && hasPaths)
    throw new Error('grep: cannot combine pipeline input with path arguments');
  if (!hasInput && !hasPaths)
    throw new Error('grep: requires either pipeline input or path arguments');

  const re = _buildRegex(pattern, opts);

  if (hasInput) {
    if (!(prev instanceof Response))
      throw new Error('grep: pipeline input must be a Response');
    const text = await prev.text();
    return responseFromText(_filterLines(text, re, opts));
  }

  const results = await Promise.all(paths.map(async p => {
    if (typeof p !== 'string')
      throw new Error(`grep: path arguments must be strings, got ${typeof p}`);
    const { serverUrl, volume, path } = shell.resolvePath(p);
    const file = await shell._vol(serverUrl, volume).lookup(path);
    const text = await file.text();
    return _filterLines(text, re, opts);
  }));

  return responseFromText(results.filter(Boolean).join('\n'));
}

function _buildRegex(pattern, opts) {
  if (pattern instanceof RegExp) return pattern;
  const flags = opts.ignoreCase ? 'i' : '';
  if (opts.fixed) {
    const escaped = String(pattern).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(escaped, flags);
  }
  return new RegExp(String(pattern), flags);
}

function _filterLines(text, re, opts) {
  const lines = text.split('\n');
  return (opts.invert
    ? lines.filter(l => !re.test(l))
    : lines.filter(l =>  re.test(l))
  ).join('\n');
}