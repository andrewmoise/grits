function matchGlob(pattern, name) {
  const re = pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '\x00')
    .replace(/\*/g, '[^/]*')
    .replace(/\?/g, '[^/]')
    .replace(/\x00/g, '.*');
  return new RegExp(`^${re}$`).test(name);
}

async function expand(parentFile, parts, partial, results) {
  if (!parentFile.isDir()) return;

  const [pattern, ...rest] = parts;

  let listing;
  try {
    listing = await parentFile.json();
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
      }
    }
  }
}

export async function glob(gimbal, pattern) {
  const resolved = gimbal.resolvePath(pattern);
  const volumeName = resolved.volumeName;
  const pathStr    = resolved.path;
  let resultPrefix = '';

  if (pattern.startsWith('//')) {
    resultPrefix = `//${volumeName}/`;
  } else {
    resultPrefix = '/';
  }

  const allParts  = pathStr ? pathStr.split('/').filter(Boolean) : [];
  const firstWild = allParts.findIndex(p => /[*?]/.test(p));

  const fixedParts = firstWild === -1 ? allParts           : allParts.slice(0, firstWild);
  const wildParts  = firstWild === -1 ? []                 : allParts.slice(firstWild);
  const fixedPath  = fixedParts.join('/');

  let startFile;
  try {
    const vol = gimbal.grits.volume(gimbal._serverUrl, volumeName);
    startFile = await vol.lookup(fixedPath);
  } catch (e) {
    return [];
  }

  if (wildParts.length === 0) {
    const rel = fixedParts.join('/');
    return [resultPrefix + rel];
  }

  const rawResults = [];
  await expand(startFile, wildParts, fixedPath, rawResults);

  return rawResults.map(r => resultPrefix + r);
}
