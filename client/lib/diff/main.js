import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
diff — compare two filesystem paths

Usage:
  pathA.diff(pathB)                top-level only
  pathA.diff(pathB, {r:1})         recursive
  gsh.diff('/a', '/b')            same

Output is JSONL: each line is [path, cid_a, cid_b]. null means absent.`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    const p = args.find(a => a instanceof GimbalPath);
    if (p) return p;
    const str = args.find(a => typeof a === 'string');
    if (str) return new GimbalPath('/' + prev.resolvePath(str).path, prev);
  }
  return null;
}

function findPathB(args) {
  const path = args.find(a => a instanceof GimbalPath);
  if (path) return path;
  const result = args.find(a => a instanceof GimbalResult);
  if (result) return result;
  return null;
}

function findOpts(args) {
  return args.find(a => typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) || {};
}

export function invoke(prev, ...args) {
  const pathA = resolvePath(prev, args);
  if (!(pathA instanceof GimbalPath)) throw new Error('diff: need two paths');

  const pathB = findPathB(args);
  if (!pathB) throw new Error('diff: need two paths');

  if (pathB instanceof GimbalResult) {
    return new GimbalResult(async () => {
      const resolved = await pathB;
      const remaining = args.filter(a => a !== pathB);
      return invoke(prev, resolved, ...remaining);
    });
  }

  const opts = findOpts(args);
  const shell = pathA._shell;
  return new GimbalResult(async () => {
    const rA = pathA._shell.resolvePath(pathA.abs());
    const rB = pathB._shell.resolvePath(pathB.abs());
    const volA = shell._vol(rA.serverUrl, rA.volume);
    const volB = shell._vol(rB.serverUrl, rB.volume);
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
