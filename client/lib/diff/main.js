// lib/diff/main.js
export const help = `\
diff — compare two filesystem paths

Usage:
  diff('path/a', 'path/b')                     top-level only
  diff('path/a', 'path/b', {r:1})              recursive

Output is JSONL: each line is [path, cid_a, cid_b].
null means the entry is absent on that side.`;

import { isVoid, _isPlainObject, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev)) throw new Error('diff: does not accept pipeline input');

  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];

  if (positional.length !== 2 ||
      typeof positional[0] !== 'string' ||
      typeof positional[1] !== 'string')
    throw new Error('diff: expected diff(pathA, pathB)');

  const [pathA, pathB] = positional;

  const rA   = shell.resolvePath(pathA);
  const rB   = shell.resolvePath(pathB);
  const volA = shell._vol(rA.serverUrl, rA.volume);
  const volB = shell._vol(rB.serverUrl, rB.volume);

  const fileA = await volA.lookup(rA.path);
  const fileB = await volB.lookup(rB.path);

  const lines = [];
  await _diff(fileA, fileB, '.', opts, lines);
  return responseFromText(lines.map(l => JSON.stringify(l)).join('\n'));
}

async function _diff(left, right, relPath, opts, out) {
  const cidA = left.cid();
  const cidB = right.cid();

  if (cidA === cidB) return;

  const bothDirsRecurse = opts.r && left.isDir() && right.isDir();
  if (!bothDirsRecurse)
    out.push([relPath, cidA, cidB]);

  if (bothDirsRecurse) {
    const [childrenA, childrenB] = await Promise.all([
      left.children(),
      right.children(),
    ]);

    const names = [...new Set([...childrenA.keys(), ...childrenB.keys()])].sort();

    for (const name of names) {
      const childA   = childrenA.get(name);
      const childB   = childrenB.get(name);
      const childPath = relPath === '.' ? name : `${relPath}/${name}`;

      if (!childA) {
        out.push([childPath, null, childB.cid()]);
        if (childB.isDir()) await _missingAll(childB, childPath, 'right', out);
        continue;
      }
      if (!childB) {
        out.push([childPath, childA.cid(), null]);
        if (childA.isDir()) await _missingAll(childA, childPath, 'left', out);
        continue;
      }

      await _diff(childA, childB, childPath, opts, out);
    }
  }
}

async function _missingAll(file, relPath, side, out) {
  const children = await file.children();
  for (const [name, child] of children) {
    const childPath = relPath === '.' ? name : `${relPath}/${name}`;
    if (side === 'left') {
      out.push([childPath, child.cid(), null]);
    } else {
      out.push([childPath, null, child.cid()]);
    }
    if (child.isDir()) await _missingAll(child, childPath, side, out);
  }
}
