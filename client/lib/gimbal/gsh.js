// gsh.js — Gimbal Shell
//
// Usage:
//   import { makeShell } from './gsh.js';
//   const gsh = makeShell({ gg, gwm: null, serverUrl: '...', volume: '...' });
//
// Chain model: each command returns a new shell with its output as the next
// command's input (analogous to stdin). Commands do not take path arguments
// when they can read from input instead.
//
//   await gsh.ls()                        — list cwd
//   await gsh.lo('sub').ls()              — list a subdirectory
//   await gsh.ls().filter(x => ...)       — filter a listing
//   await gsh.lo('file.txt').cat()        — print a file
//   await gsh.lo('file.txt').li('dest')   — copy by re-linking
//   await gsh('hello', 'world')           — run lib/hello/main.js
//
// Coercion chain (automatic, one direction):
//   GimbalPath → GritsFile → GimbalStream → JS object

import { GritsFile } from './GritsClient.js'; // %FOR MODULE%
//const { GritsFile } = self.Grits;            // %FOR SERVICEWORKER%

// ─────────────────────────────────────────────────────────────────
// Shell-layer output types
// ─────────────────────────────────────────────────────────────────

// GimbalPath: an unresolved path reference within a server+volume
export class GimbalPath {
  constructor(path, serverUrl = null, volume = null) {
    this.path      = path;
    this.serverUrl = serverUrl;
    this.volume    = volume;
  }
  toString() {
    if (this.serverUrl && this.volume) return `${this.serverUrl}/${this.volume}:${this.path}`;
    if (this.volume) return `${this.volume}:${this.path}`;
    return this.path;
  }
}

// GimbalStream: a Response with extra provenance metadata
export class GimbalStream {
  constructor(response, { source = null } = {}) {
    this._response   = response;
    this.contentType = response.headers.get('content-type');
    this.size        = response.headers.get('content-length')
      ? parseInt(response.headers.get('content-length')) : null;
    this.source      = source; // GimbalPath, GritsFile, or CID string
  }
  get response() { return this._response; }
  async bytes()  { return this._response.arrayBuffer(); }
  async text()   { return this._response.text(); }
  async json()   { return this._response.json(); }
}

// ─────────────────────────────────────────────────────────────────
// Session — shared mutable state across a chain
// ─────────────────────────────────────────────────────────────────

class GimbalSession {
  constructor({ gg, gwm = null, serverUrl = null, volume = null, cwd = '', mounts = {} }) {
    this.gg        = gg;
    this.gwm       = gwm;
    this.serverUrl = serverUrl;
    this.volume    = volume;
    this.cwd       = cwd;
    this.mounts    = mounts;
    this.jobs      = [];
  }

  resolvePath(p) {
    if (!p || p === '.') return this.cwd;
    if (p.startsWith('/')) return p.replace(/^\//, '');
    if (!this.cwd) return p;
    return `${this.cwd}/${p}`.replace(/\/+/g, '/');
  }

  _sv() { if (!this.serverUrl) throw new Error('no serverUrl on session'); return this.serverUrl; }
  _vo() { if (!this.volume)    throw new Error('no volume on session');    return this.volume; }
}

// ─────────────────────────────────────────────────────────────────
// Coercion: GimbalPath → GritsFile → GimbalStream → JS object
// ─────────────────────────────────────────────────────────────────

async function coerceToFile(value, session) {
  if (value instanceof GritsFile)  return value;
  if (value instanceof GimbalPath) {
    return session.gg.lo(
      value.serverUrl ?? session._sv(),
      value.volume    ?? session._vo(),
      value.path
    );
  }
  if (typeof value === 'string') {
    // Treat as a metadata CID
    const meta = await session.gg.meta(value);
    return new GritsFile(value, meta, session.gg);
  }
  throw new TypeError(`Cannot coerce ${value?.constructor?.name ?? typeof value} to GritsFile`);
}

async function coerceToStream(value, session) {
  if (value instanceof GimbalStream) return value;
  if (value instanceof GritsFile)
    return new GimbalStream(await value.get(), { source: value });
  if (value instanceof GimbalPath || typeof value === 'string') {
    const file = await coerceToFile(value, session);
    return new GimbalStream(await file.get(), { source: file });
  }
  throw new TypeError(`Cannot coerce ${value?.constructor?.name ?? typeof value} to GimbalStream`);
}

async function coerceToJS(value, session) {
  if (value instanceof GimbalStream) return value.json();
  if (value instanceof GritsFile)    return value.json();
  if (value instanceof GimbalPath || typeof value === 'string')
    return (await coerceToStream(value, session)).json();
  return value;
}

export { coerceToFile, coerceToStream, coerceToJS };

// ─────────────────────────────────────────────────────────────────
// Module loading from source string
// ─────────────────────────────────────────────────────────────────

async function _evalModule(src) {
  const blob = new Blob([src], { type: 'text/javascript' });
  const url  = URL.createObjectURL(blob);
  try    { return await import(url); }
  finally { URL.revokeObjectURL(url); }
}

// ─────────────────────────────────────────────────────────────────
// Shell — immutable chain node
// ─────────────────────────────────────────────────────────────────

class GimbalShell {
  constructor(session, _input = null) {
    this._session = session;
    this._input   = _input; // Promise<any> | null
  }

  _next(p) { return new GimbalShell(this._session, Promise.resolve(p)); }

  // ── read input ────────────────────────────────────────────────

  async read_void() {
    if (this._input !== null) {
      const v = await this._input;
      if (v !== undefined && v !== null)
        throw new Error('read_void: unexpected input');
    }
    return undefined;
  }

  async read_js()         {
    if (this._input === null) throw new Error('read_js: no input');
    return coerceToJS(await this._input, this._session);
  }
  async read_bytestream() {
    if (this._input === null) throw new Error('read_bytestream: no input');
    return coerceToStream(await this._input, this._session);
  }
  async read_file()       {
    if (this._input === null) throw new Error('read_file: no input');
    return coerceToFile(await this._input, this._session);
  }
  async read_path() {
    if (this._input === null) throw new Error('read_path: no input');
    const v = await this._input;
    if (!(v instanceof GimbalPath))
      throw new TypeError(`read_path: got ${v?.constructor?.name ?? typeof v}`);
    return v;
  }

  // ── background ───────────────────────────────────────────────

  bg() {
    if (this._input === null) return this;
    const idx = this._session.jobs.length;
    const job = { promise: this._input, done: false, result: undefined, error: undefined };
    this._input.then(
      r => { job.done = true; job.result = r; },
      e => { job.done = true; job.error  = e; console.warn(`[gsh job ${idx} error]`, e); }
    );
    this._session.jobs.push(job);
    console.log(`[${idx}] background`);
    return new GimbalShell(this._session, null);
  }

  // ── filesystem ───────────────────────────────────────────────

  // ls: no args — lists input (if given) or cwd.
  // Input must be a GritsFile of a directory, or a GimbalPath/CID resolving to one.
  ls() {
    return this._next((async () => {
      if (this._input !== null) {
        // ls of whatever was piped in
        const file = await coerceToFile(await this._input, this._session);
        if (!file.isDir()) throw new Error('ls: input is not a directory');
        return file.json();
      }
      // ls of cwd
      const file = await this._session.gg.lo(
        this._session._sv(), this._session._vo(),
        this._session.cwd || '.'
      );
      if (!file.isDir()) throw new Error('ls: cwd is not a directory');
      return file.json();
    })());
  }

  // cat: no args — prints input as text.
  cat() {
    return this._next((async () => {
      if (this._input === null) throw new Error('cat: no input');
      const stream = await coerceToStream(await this._input, this._session);
      const text   = await stream.text();
      console.log(text);
      return text;
    })());
  }

  // cd: updates cwd. Verifies path exists.
  cd(path) {
    return this._next((async () => {
      const resolved = this._session.resolvePath(path);
      await this._session.gg.lo(this._session._sv(), this._session._vo(), resolved);
      this._session.cwd = resolved;
      return new GimbalPath(resolved, this._session.serverUrl, this._session.volume);
    })());
  }

  // lo: resolve a path to a GritsFile (no network call yet for the content)
  lo(path) {
    return this._next((async () => {
      return this._session.gg.lo(
        this._session._sv(), this._session._vo(),
        this._session.resolvePath(path)
      );
    })());
  }

  // li: link input into the filesystem at pathArg.
  // GritsFile or CID string → re-link pointer only (no upload).
  // Bytes/stream/string/object → put + mkfile + li.
  li(pathArg) {
    return this._next((async () => {
      if (this._input === null) throw new Error('li: no input');
      const dest  = this._session.resolvePath(pathArg);
      const sv    = this._session._sv();
      const vo    = this._session._vo();
      const input = await this._input;

      if (input instanceof GritsFile) {
        await this._session.gg.li(input, sv, vo, dest);
        return new GimbalPath(dest, sv, vo);
      }

      if (input instanceof GimbalPath) {
        const file = await coerceToFile(input, this._session);
        await this._session.gg.li(file, sv, vo, dest);
        return new GimbalPath(dest, sv, vo);
      }

      if (typeof input === 'string' && !input.includes('/')) {
        // Raw metadata CID string
        await this._session.gg.li(input, sv, vo, dest);
        return new GimbalPath(dest, sv, vo);
      }

      // Bytes — put + mkfile + li
      let bytes;
      if (input instanceof GimbalStream)                               bytes = await input.bytes();
      else if (input instanceof ArrayBuffer || ArrayBuffer.isView(input)) bytes = input;
      else if (typeof input === 'string')                              bytes = new TextEncoder().encode(input);
      else                                                             bytes = new TextEncoder().encode(JSON.stringify(input));

      const byteArray  = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
      const contentCID = await this._session.gg.put(byteArray);
      const metaCID    = await this._session.gg.mkfile(contentCID, byteArray.byteLength);
      await this._session.gg.li(metaCID, sv, vo, dest);
      return new GimbalPath(dest, sv, vo);
    })());
  }

  // ── filter ───────────────────────────────────────────────────

  filter(fn) {
    return this._next((async () => {
      const input = await coerceToJS(await this._input, this._session);
      if (Array.isArray(input))
        return input.filter(fn);
      if (typeof input === 'object' && input !== null)
        return Object.fromEntries(Object.entries(input).filter(([k, v]) => fn(k, v)));
      throw new TypeError('filter: input must be array or object');
    })());
  }

  // ── app dispatch ─────────────────────────────────────────────
  // Looks for lib/{name}/main.js in the current volume.

  run(name, ...args) {
    return this._next((async () => {
      const { gg } = this._session;
      const sv     = this._session._sv();
      const vo     = this._session._vo();
      let mod;

      const mainPath = this._session.resolvePath(`lib/${name}/main.js`);
      try {
        const file = await gg.lo(sv, vo, mainPath);
        const resp = await file.get();
        if (resp.ok) mod = await _evalModule(await resp.text());
      } catch (_) {}

      if (!mod) throw new Error(`command not found: ${name}`);
      if (typeof mod.run !== 'function')
        throw new Error(`lib/${name}/main.js has no exported run()`);

      return mod.run(new GimbalShell(this._session, this._input), ...args);
    })());
  }

  // ── import ───────────────────────────────────────────────────
  // Load lib/{name}/index.js and return its exports.
  // Subsequent calls for the same name return the cached module.

  import(name) {
    return this._next((async () => {
      if (!this._session._importCache) this._session._importCache = new Map();
      if (this._session._importCache.has(name))
        return this._session._importCache.get(name);

      const { gg } = this._session;
      const sv     = this._session._sv();
      const vo     = this._session._vo();

      const indexPath = this._session.resolvePath(`lib/${name}/index.js`);
      const file = await gg.lo(sv, vo, indexPath);
      const resp = await file.get();
      if (!resp.ok) throw new Error(`import: lib/${name}/index.js not found`);

      const mod = await _evalModule(await resp.text());
      this._session._importCache.set(name, mod);
      return mod;
    })());
  }

  // ── thenable — so `await gsh.ls()` just works ────────────────

  then(resolve, reject) {
    return (this._input ?? Promise.resolve(undefined)).then(resolve, reject);
  }
  catch(reject) {
    return (this._input ?? Promise.resolve(undefined)).catch(reject);
  }
}

// ─────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────

export function makeShell({ gg, gwm = null, serverUrl = null, volume = null, cwd = '', mounts = {} }) {
  const session = new GimbalSession({ gg, gwm, serverUrl, volume, cwd, mounts });
  const root    = new GimbalShell(session, null);

  // callable: gsh('cmd', ...args) → gsh.run('cmd', ...args)
  return new Proxy(root, {
    apply(_t, _this, [name, ...args]) { return root.run(name, ...args); },
    get(t, k) { return t[k]; },
  });
}
