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

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pattern, ...paths] = positional;

  if (pattern === undefined)
    throw new Error('grep: pattern required');

  const prev = await previous;
  const hasInput = !isVoid(prev);
  const hasPaths = paths.length > 0;

  if (hasInput && hasPaths)
    throw new Error('grep: cannot combine pipeline input with path arguments');
  if (!hasInput && !hasPaths)
    throw new Error('grep: requires either pipeline input or path arguments');

  const re = _buildRegex(pattern, opts);

  if (hasInput) {
    const text = await _toText(prev, 'grep');
    return _filterLines(text, re, opts);
  }

  // Path arguments mode — validate all are strings, read and concatenate
  const results = await Promise.all(paths.map(async p => {
    if (typeof p !== 'string')
      throw new Error(`grep: path arguments must be strings, got ${typeof p}`);
    const { serverUrl, volume, path } = shell.resolvePath(p);  // was: value
    const file = await shell._vol(serverUrl, volume).lookup(path);  // was: missing await
    const text = await file.text();
    return _filterLines(text, re, opts);
  }));

  return results.filter(Boolean).join('\n');
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

async function _toText(value, toolName) {
  if (typeof value === 'string')   return value;
  if (value instanceof GritsFile)  return value.text();
  if (value instanceof Response)   return value.text();
  throw new Error(`${toolName}: cannot read text from ${value?.constructor?.name ?? typeof value}`);
}

function _filterLines(text, re, opts) {
  const lines = text.split('\n');
  return (opts.invert ? lines.filter(l => !re.test(l)) : lines.filter(l => re.test(l))).join('\n');
}