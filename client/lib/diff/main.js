import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalClient } from '../gimbal/client.js';

export const help = `\
diff — compare two filesystem paths

Usage:
  pathA.diff(pathB)                top-level only (pathB: GimbalPath or string)
  pathA.diff(pathB, {r:1})         recursive
  gimbal.diff(pathA, pathB)        same (paths must be GimbalPath)

Output is JSONL: each line is [path, cid_a, cid_b]. null means absent.`;

export function invoke(gimbal, prev, ...args) {
  let pathA, pathB, opts = {};

  if (prev instanceof GimbalClient) {
    let argIdx = 0;
    for (let i = 0; i < args.length; i++) {
      const a = args[i];
      if (argIdx === 0 && (a instanceof GimbalPath || typeof a === 'string')) {
        pathA = a instanceof GimbalPath ? a : gimbal.p(a);
        argIdx++;
      } else if (argIdx === 1 && (a instanceof GimbalPath || typeof a === 'string')) {
        pathB = a instanceof GimbalPath ? a : gimbal.p(a);
        argIdx++;
      } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
        opts = a;
      } else {
        throw new Error('diff: unexpected argument');
      }
    }
  } else if (prev instanceof GimbalPath) {
    pathA = prev;
    for (let i = 0; i < args.length; i++) {
      const a = args[i];
      if (i === 0 && a instanceof GimbalPath) {
        pathB = a;
      } else if (i === 0 && typeof a === 'string') {
        pathB = prev.p(a);
      } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
        opts = a;
      } else {
        throw new Error('diff: unexpected argument');
      }
    }
  } else {
    throw new Error('diff: need a path');
  }

  if (!pathA || !pathB) throw new Error('diff: need two paths');

  return new GimbalResult(async () => {
    const rA = gimbal.resolvePath(pathA.abs());
    const rB = gimbal.resolvePath(pathB.abs());
    const volA = gimbal.grits.volume(gimbal._serverUrl, rA.volumeName);
    const volB = gimbal.grits.volume(gimbal._serverUrl, rB.volumeName);
    const fileA = await volA.lookup(rA.path);
    const fileB = await volB.lookup(rB.path);
    const lines = [];
    await _diff(fileA, fileB, '.', opts, lines);
    return lines.map(l => JSON.stringify(l)).join('\n');
  });
}

async function _diff(left, right, relPath, opts, out) {
  const cidA = left.cid();
  const cidB = right.cid();
  if (cidA === cidB) return;
  const bothDirs = opts.r && left.isDir() && right.isDir();
  if (!bothDirs) out.push([relPath, cidA, cidB]);
  if (bothDirs) {
    const [ca, cb] = await Promise.all([left.children(), right.children()]);
    for (const name of [...new Set([...ca.keys(), ...cb.keys()])].sort()) {
      const childA = ca.get(name), childB = cb.get(name);
      const cp = relPath === '.' ? name : `${relPath}/${name}`;
      if (!childA) { out.push([cp, null, childB.cid()]); if (childB.isDir()) await _miss(childB, cp, out); continue; }
      if (!childB) { out.push([cp, childA.cid(), null]); if (childA.isDir()) await _miss(childA, cp, out); continue; }
      await _diff(childA, childB, cp, opts, out);
    }
  }
}

async function _miss(file, relPath, out) {
  for (const [name, child] of await file.children()) {
    const cp = `${relPath}/${name}`;
    out.push([cp, child.cid(), null]);
    if (child.isDir()) await _miss(child, cp, out);
  }
}
