// lib/from/main.js
export const help = `\
from — bring a resource into the pipeline

Usage:
  from('path/to/file')     resolve a path on the current volume
  from(':volume/path')     path on a different volume  
  from(gritsFile)          wrap an existing GritsFile
  from(response)           wrap an existing Response

Requires void input. Use as a pipeline entry point only.`;

import { coerceToFile, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('from: does not accept pipeline input — use as entry point only');

  const [source] = args;
  if (source === undefined)
    throw new Error('from: argument required');

  if (source instanceof Response) return source;
  if (source instanceof GritsFile) return source;

  // CID or path string → GritsFile
  return coerceToFile(source, shell);
}