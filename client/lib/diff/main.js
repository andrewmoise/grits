import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
diff — compare two filesystem paths

Usage:
  pathA.diff(pathB)                top-level only (pathB: GimbalPath or string)
  pathA.diff(pathB, {r:1})         recursive
  gimbal.diff(pathA, pathB)          same (paths must be GimbalPath)

Output is JSONL: each line is [path, cid_a, cid_b]. null means absent.`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  return args.find(a => a instanceof GimbalPath) || null;
}

function findPathB(args, gimbal) {
  const p = args.find(a => a instanceof GimbalPath);
  if (p) return p;
  const str = args.find(a => typeof a === 'string');
  if (str && gimbal) {
    const res = gimbal.resolvePath(str);
    return gimbal.p('/' + res.path);
  }
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath)) || {};
}

export function invoke(gimbal, prev, ...args) {
  const pathA = resolvePath(prev, args);
  if (!(pathA instanceof GimbalPath)) throw new Error('diff: need two paths');

  const pathB = findPathB(args, gimbal);
  if (!pathB) throw new Error('diff: need two paths');

  const opts = findOpts(args);
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
