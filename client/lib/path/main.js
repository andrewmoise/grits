import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
path — resolve a path to a GimbalPath object

Usage:
  gsh.p('/absolute/path')     returns p(/path)
  path.p('relative')          resolve relative to path`;

export function invoke(prev, relative) {
  if (prev instanceof GimbalShell) {
    const r = prev.resolvePath(relative);
    return new GimbalResult(() => new GimbalPath('/' + r.path, prev));
  }
  if (prev instanceof GimbalPath) {
    const resolved = _resolveRelative(prev.abs(), String(relative));
    return new GimbalResult(() => new GimbalPath(resolved, prev._shell));
  }
  if (prev instanceof GimbalResult) {
    return new GimbalResult(async () => {
      const resolved = await prev;
      return invoke(resolved, relative);
    });
  }
  throw new Error('path: called on unexpected value type');
}

function _resolveRelative(base, relative) {
  const baseParts = base.split('/').filter(Boolean);
  const relParts = relative.split('/').filter(Boolean);
  for (const part of relParts) {
    if (part === '.') continue;
    if (part === '..') { if (baseParts.length > 0) baseParts.pop(); continue; }
    baseParts.push(part);
  }
  return '/' + baseParts.join('/');
}
