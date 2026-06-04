// lib/unzip/main.js
export const help = `\
unzip — extract a zip archive into the current directory

Usage:
  unzip('file.zip')        extract file into cwd
  <file>.unzip()           extract from pipeline input
  unzip('file.zip', {f:1}) overwrite existing files`;

import { isVoid, VOID, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile, AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';
import { Unzip, AsyncUnzipInflate } from 'https://esm.sh/fflate';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pathArg]  = positional;

  const prev = await previous;

  let file;
  if (!isVoid(prev)) {
    if (pathArg !== undefined)
      throw new Error('unzip: cannot combine pipeline input with path argument');
    if (!(prev instanceof GritsFile))
      throw new Error('unzip: pipeline input must be a GritsFile');
    file = prev;
  } else {
    if (pathArg !== undefined) {
      if (typeof pathArg !== 'string')
        throw new Error('unzip: path argument must be a string');
      const { serverUrl, volume, path } = shell.resolvePath(pathArg);
      file = await shell._vol(serverUrl, volume).lookup(path);
    } else {
      throw new Error('unzip: expected a file or pipeline input');
    }
  }

  if (!file.isFile())
    throw new Error('unzip: not a file');

  const { serverUrl, volume, path: cwdPath } = shell.resolvePath('.');
  const vol = shell._vol(serverUrl, volume);
  // streaming unzip
  let chain = Promise.resolve();

  const unzip = new Unzip((fileEntry) => {
    const rawPath = fileEntry.name || '';
    const clean = rawPath.replace(/^\/+/, '');
    if (!clean) return fileEntry.terminate();

    const parts = clean.split('/').filter(Boolean);
    if (parts.length === 0) return fileEntry.terminate();

    const isDir = rawPath.endsWith('/');

    let currentPath = cwdPath;

    // ensure directories (mkdir -p semantics)
    const ensureDirs = async (dirParts) => {
      for (const part of dirParts) {
        currentPath = currentPath.replace(/\/?$/, '/') + part;

        const metaCID = await vol.mkdir({});

        try {
          await vol.multiLink([{
            path: currentPath,
            addr: metaCID,
            prevAddr: '',
            assert: ASSERT_PREV_MATCHES,
          }]);
          continue;
        } catch (e) {
          if (!(e instanceof AssertionError)) throw e;
        }

        if (opts.f) {
          try {
            await vol.multiLink([{
              path: currentPath,
              addr: metaCID,
              assert: ASSERT_IS_BLOB,
            }]);
            continue;
          } catch (e) {
            if (!(e instanceof AssertionError)) throw e;
          }
        }

        const existing = await vol.lookup(currentPath).catch(() => null);
        if (existing && !existing.isDir()) {
          throw new Error(`unzip: not a directory: '${currentPath}'`);
        }
      }
    };

    chain = chain.then(async () => {
      const dirParts = isDir ? parts : parts.slice(0, -1);
      await ensureDirs(dirParts);

      if (isDir) return;

      const filePath = currentPath.replace(/\/?$/, '/') + parts[parts.length - 1];

      const buf = await new Promise((resolve, reject) => {
        const chunks = [];
        fileEntry.ondata = (err, data, final) => {
          if (err) return reject(err);
          if (data) chunks.push(data);
          if (final) {
            const total = chunks.reduce((n, c) => n + c.length, 0);
            const out = new Uint8Array(total);
            let off = 0;
            for (const c of chunks) {
              out.set(c, off);
              off += c.length;
            }
            resolve(out);
          }
        };
        fileEntry.start();
      });

      const contentCID = await vol.put(buf);
      const metaCID = await vol.mkfile(contentCID, buf.byteLength);

      try {
        await vol.multiLink([{
          path: filePath,
          addr: metaCID,
          prevAddr: opts.f ? undefined : '',
          assert: opts.f ? 0 : ASSERT_PREV_MATCHES,
        }]);
      } catch (e) {
        if (!opts.f && e instanceof AssertionError)
          throw new Error(`unzip: file exists: '${filePath}' — use {f:1} to overwrite`);
        throw e;
      }
    });
  });

  unzip.register(AsyncUnzipInflate);

  const resp = await file.get();
  const reader = resp.body.getReader();

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    unzip.push(value);
  }
  unzip.push(new Uint8Array(0), true);

  // wait for all entries to finish (and surface errors)
  await chain;

  return VOID;
}
