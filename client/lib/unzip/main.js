import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { AssertionError, ASSERT_PREV_MATCHES, ASSERT_IS_BLOB } from '../grits/GritsClient.js';
import { Unzip, AsyncUnzipInflate } from '/lib/node_modules/fflate/esm/browser.js';

export const help = `\
unzip — extract a zip archive into the current directory

Usage:
  path.unzip()                 extract zip into cwd
  path.unzip({f:1})            overwrite existing files
  gimbal.unzip(path)              same (path must be GimbalPath)`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath)) throw new Error('unzip: need a file path');

  let target = prev;
  let opts = {};

  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      target = prev.p(a);
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('unzip: unexpected argument');
    }
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(target._path);
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const file = await vol.lookup(r.path);
    if (!file.isFile()) throw new Error('unzip: not a file');

    const cwdPath = gimbal.resolvePath('.').path;
    let chain = Promise.resolve();

    const unzip = new Unzip((fileEntry) => {
      const rawPath = fileEntry.name || '';
      const clean = rawPath.replace(/^\/+/, '');
      if (!clean) return fileEntry.terminate();
      const parts = clean.split('/').filter(Boolean);
      if (parts.length === 0) return fileEntry.terminate();
      const isDir = rawPath.endsWith('/');
      let curPath = cwdPath;

      chain = chain.then(async () => {
        const dp = isDir ? parts : parts.slice(0, -1);
        for (const part of dp) {
          curPath = curPath.replace(/\/?$/, '/') + part;
          const m = await vol.mkdir({});
          try { await vol.multiLink([{ path: curPath, addr: m, prevAddr: '', assert: ASSERT_PREV_MATCHES }]); continue; }
          catch (e) { if (!(e instanceof AssertionError)) throw e; }
          if (opts.f) {
            try { await vol.multiLink([{ path: curPath, addr: m, assert: ASSERT_IS_BLOB }]); continue; }
            catch (e) { if (!(e instanceof AssertionError)) throw e; }
          }
          const ex = await vol.lookup(curPath).catch(() => null);
          if (ex && !ex.isDir()) throw new Error(`unzip: not a dir '${curPath}'`);
        }
        if (isDir) return;

        const fp = curPath.replace(/\/?$/, '/') + parts[parts.length - 1];
        const buf = await new Promise((resolve, reject) => {
          const chunks = [];
          fileEntry.ondata = (err, data, final) => {
            if (err) return reject(err);
            if (data) chunks.push(data);
            if (final) {
              const total = chunks.reduce((n, c) => n + c.length, 0);
              const out = new Uint8Array(total); let off = 0;
              for (const c of chunks) { out.set(c, off); off += c.length; }
              resolve(out);
            }
          };
          fileEntry.start();
        });

        const cc = await vol.put(buf);
        const mc = await vol.mkfile(cc, buf.byteLength);
        try { await vol.multiLink([{ path: fp, addr: mc, prevAddr: opts.f ? undefined : '', assert: opts.f ? 0 : ASSERT_PREV_MATCHES }]); }
        catch (e) { if (!opts.f && e instanceof AssertionError) throw new Error(`unzip: exists '${fp}' — use {f:1}`); throw e; }
      });
    });

    unzip.register(AsyncUnzipInflate);
    const resp = await file.get();
    const reader = resp.body.getReader();
    while (true) { const { done, value } = await reader.read(); if (done) break; unzip.push(value); }
    unzip.push(new Uint8Array(0), true);
    await chain;
  });
}
