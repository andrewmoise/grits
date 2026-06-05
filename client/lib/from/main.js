// lib/from/main.js
export const help = `\
from — bring a single resource into the pipeline, preserving its type

Usage:
  from('path/to/file')     → GritsFile
  from('//volume/path')    → GritsFile
  from(gritsFile)          → GritsFile (pass-through)
  from(response)           → Response (pass-through)

Requires void input and exactly one argument.
For concatenation or bytestream output, use cat() instead.
For arrays of files, from([...]) will be supported in future.`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('from: does not accept pipeline input — use cat() for concatenation');

  const [source] = args;
  if (source === undefined)
    throw new Error('from: exactly one argument required');
  if (args.length > 1)
    throw new Error('from: exactly one argument required — use cat() to concatenate multiple files');

  if (source instanceof Response) return source;
  if (source instanceof GritsFile) return source;

  if (typeof source === 'string') {
    const { serverUrl, volume, path } = shell.resolvePath(source);
    return shell._vol(serverUrl, volume).lookup(path);
  }

  throw new TypeError(
    `from: expected a path string, GritsFile, or Response, got ${source?.constructor?.name ?? typeof source}`);
}