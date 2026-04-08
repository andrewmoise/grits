// lib/gimbal/glob.js

import { _isPlainObject } from './gsh.js';

function matchGlob(pattern, name) {
  const re = pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '\x00')
    .replace(/\*/g, '[^/]*')
    .replace(/\?/g, '[^/]')
    .replace(/\x00/g, '.*');
  return new RegExp(`^${re}$`).test(name);
}

async function expand(shell, r, results, parts, partial) {
  if (parts.length === 0) {
    results.push(partial);
    return;
  }

  const [pattern, ...rest] = parts;
  const vol = shell._vol(r.serverUrl, r.volume);

  try {
    const { serverUrl, volume, path } =
      (partial === '')
      ? shell.resolvePath(shell.cwd)
      : shell.resolvePath(partial);
    const vol = shell.fs.volume(serverUrl, volume);

    const file = await vol.lookup(path);
    if (!file.isDir()) return;
    const dir = await file.json();
    for (const name of Object.keys(dir)) {
      if (matchGlob(pattern, name)) {
        const childPath = partial ? `${partial}/${name}` : name;
        await expand(shell, r, results, rest, childPath);
      }
    }
  } catch (e) {
    return;
  }
}

export async function glob(shell, pattern) {
  const parts  = pattern.split('/').filter(Boolean);
  const results = [];
  await expand(shell, pattern, results, parts, '');
  return results;
}