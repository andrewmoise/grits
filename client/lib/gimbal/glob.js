// lib/gimbal/glob.js

function matchGlob(pattern, name) {
  const re = pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '\x00')
    .replace(/\*/g, '[^/]*')
    .replace(/\?/g, '[^/]')
    .replace(/\x00/g, '.*');
  return new RegExp(`^${re}$`).test(name);
}

// expand — purely structural, no path resolution.
// parentFile : GritsFile for the current directory
// parts      : remaining pattern segments to match
// partial    : path built so far, relative to the root passed by glob()
// results    : string accumulator
async function expand(parentFile, parts, partial, results) {
  if (!parentFile.isDir()) return;

  const [pattern, ...rest] = parts;

  let listing;
  try {
    listing = await parentFile.json(); // { name: metaCID, ... }
  } catch (e) {
    return;
  }

  for (const name of Object.keys(listing)) {
    if (!matchGlob(pattern, name)) continue;

    const childPartial = partial ? `${partial}/${name}` : name;

    if (rest.length === 0) {
      results.push(childPartial);
    } else {
      try {
        const childFile = await parentFile._volume.fileForCID(listing[name]);
        await expand(childFile, rest, childPartial, results);
      } catch (e) {
        // skip inaccessible children
      }
    }
  }
}

export async function glob(shell, pattern) {
  // ── 1. Parse prefix → serverUrl, volume, pathStr ───────────
  //
  // Three forms:
  //   '//volume[/path]'          — explicit volume, current server
  //   '//server:volume[/path]' — explicit server + volume
  //   '/path'                    — absolute, current volume
  //   'path'                     — relative to cwd

  let serverUrl;
  let volume;
  let pathStr;
  let resultPrefix = ''; // prepended to each result

  const resolved = shell.resolvePath(pattern);
  serverUrl = resolved.serverUrl;
  volume    = resolved.volume;
  pathStr   = resolved.path;

  // Validate: no wildcards in server or volume
  if (/[*?]/.test(resolved.serverUrl) || /[*?]/.test(resolved.volume)) {
    throw new Error('wildcards not allowed in server or volume');
  }

  // Reconstruct prefix from resolved path
  if (pattern.startsWith('//')) {
    if (resolved.serverUrl !== shell.serverUrl) {
      resultPrefix = `//${resolved.serverUrl}:${resolved.volume}/`;
    } else {
      resultPrefix = `//${resolved.volume}/`;
    }
  } else if (pattern.startsWith('/')) {
    resultPrefix = '/';
  } else {
    resultPrefix = null; // relative
  }

  // ── 2. Split into fixed prefix + wildcard parts ────────────
  const allParts  = pathStr ? pathStr.split('/').filter(Boolean) : [];
  const firstWild = allParts.findIndex(p => /[*?]/.test(p));

  const fixedParts = firstWild === -1 ? allParts           : allParts.slice(0, firstWild);
  const wildParts  = firstWild === -1 ? []                 : allParts.slice(firstWild);
  const fixedPath  = fixedParts.join('/'); // '' = volume root

  // ── 3. Resolve fixed prefix to a GritsFile ─────────────────
  let startFile;
  try {
    const vol = shell.fs.volume(serverUrl, volume);
    startFile = await vol.lookup(fixedPath);
  } catch (e) {
    return [];
  }

  // ── 4. No wildcards — fixed lookup is the whole answer ──────
  if (wildParts.length === 0) {
    const rel = fixedParts.join('/');
    return [_applyPrefix(resultPrefix, rel, shell)];
  }

  // ── 5. Expand wildcards ─────────────────────────────────────
  const rawResults = [];
  await expand(startFile, wildParts, fixedPath, rawResults);

  return rawResults.map(r => _applyPrefix(resultPrefix, r, shell));
}

function _applyPrefix(prefix, path, shell) {
  if (prefix !== null) return prefix + path;
  // relative: strip cwd prefix
  const cwd = shell.cwd.replace(/^\/+|\/+$/g, '');
  if (cwd && path.startsWith(cwd + '/')) return path.slice(cwd.length + 1);
  if (path === cwd) return '.';
  return path;
}
