// gsh.js — Gimbal Shell
//
// Architecture:
//   GimbalShell — one per session. Holds cwd, history, lib URLs, import
//                 cache. This is what xterm.js (or any REPL host) holds.
//
//   Result      — one per pipeline stage. Wraps a Promise<value> and a ref
//                 to the shell. Thenable so tools can `await previous`.
//                 Always returned wrapped in a dispatch Proxy so that unknown
//                 method calls auto-dispatch to lib/<n>/main.js.
//
//                 Terminal methods (cross the boundary to plain JS values):
//                   .toText()     → Promise<string>
//                   .toBytes()    → Promise<Uint8Array>
//                   .toResponse() → Promise<Response>
//                   .toFile()     → Promise<GritsFile>
//                   .toCID()      → Promise<GritsCID>
//                   .toJS()       → Promise<any>
//
//                 .toString() returns a non-blocking descriptive string,
//                 safe for use in string contexts without triggering I/O.
//
//   Path        — an unresolved path reference within a server+volume.
//                 Coerces to GritsFile on demand.
//
// Tool contract:
//   Each tool is a standard ES module served from a known URL. It exports:
//     export async function invoke(shell, previous, args) { ... }
//   - shell    : GimbalShell
//   - previous : Result (thenable)
//   - args     : array of user-supplied arguments
//   - returns  : any value or Promise<value>
//
//   Options convention: if the last argument is a plain object it is treated
//   as an options map. Positional args occupy all slots before it.
//
//   Help convention: export const help = '...' for usage text.
//   Calling a tool with ({help: true}) as the only arg prints that string.
//
// Tool discovery:
//   shell.libUrls is an array of base URLs searched in order.
//   For command 'grep': tries <base>/grep/main.js, first match wins.
//   The browser's native import() handles caching and relative imports inside
//   tool modules automatically.
//
// Type cascade (one direction only):
//   GritsCID → GritsFile → Response → ArrayBuffer → string → JS object
//
// Path syntax (scp-style):
//   'lib/grits'                     — relative path, current server+volume
//   '/lib/grits'                    — absolute path, current server+volume
//   ':client/lib'                   — different volume, same server
//   ':client'                       — different volume root, same server
//   'test.melanic.org:client/lib'   — different server+volume
//
// Eval model:
//   shell.eval(src) evals src via new Function() (sloppy mode) with a
//   `with` Proxy so unknown identifiers dispatch to lib/<n>/main.js.
//   Special names wired into the eval context:
//     cd(path)    — built-in, updates cwd (and volume if :vol syntax used)
//     cid(addr)   — constructs a GritsCID
//     $(result)   — unwraps a Result to its raw Promise<value>

import { GritsFile } from '../grits/GritsClient.js';

// ─────────────────────────────────────────────────────────────────
// GritsCID — explicit CID wrapper, top of the type cascade
// ─────────────────────────────────────────────────────────────────

export class GritsCID {
  constructor(addr) {
    if (typeof addr !== 'string' || !addr)
      throw new TypeError(`GritsCID: expected non-empty string, got ${_tn(addr)}`);
    this.addr = addr;
  }
  toString() { return `GritsCID(${this.addr})`; }
  valueOf()  { return this.addr; }
}

export function cid(addr) { return new GritsCID(addr); }

// ─────────────────────────────────────────────────────────────────
// Path — unresolved path reference within a server+volume
// ─────────────────────────────────────────────────────────────────

export class Path {
  constructor(path, serverUrl = null, volume = null) {
    this.path      = path;
    this.serverUrl = serverUrl;
    this.volume    = volume;
  }
  toString() {
    if (this.serverUrl && this.volume)
      return `Path(${this.serverUrl}/${this.volume}:${this.path})`;
    if (this.volume) return `Path(${this.volume}:${this.path})`;
    return `Path(${this.path})`;
  }
}

// ─────────────────────────────────────────────────────────────────
// Void sentinel — returned by commands with no meaningful output
// ─────────────────────────────────────────────────────────────────

export const VOID = Object.freeze({ _gimbalVoid: true, toString: () => '(void)' });
export function isVoid(v) { return v === null || v === undefined || v === VOID; }

// ─────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────

function _tn(v) {
  if (v === null)      return 'null';
  if (v === undefined) return 'undefined';
  return v?.constructor?.name ?? typeof v;
}

export function _isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

// ─────────────────────────────────────────────────────────────────
// Coercion cascade
// GritsCID → GritsFile → Response → ArrayBuffer → string → JS object
// ─────────────────────────────────────────────────────────────────

export async function coerceToFile(value, shell) {
  if (value instanceof Result)    value = await value;
  if (value instanceof GritsFile) return value;
  if (value instanceof GritsCID) {
    const meta = await shell._currentVol().meta(value.addr);
    return new GritsFile(value.addr, meta, shell._currentVol());
  }
  if (value instanceof Path)
    return shell._vol(value.serverUrl, value.volume).lo(value.path);
  if (typeof value === 'string')
    return shell._currentVol().lo(shell.resolvePath(value));
  throw new TypeError(`Cannot coerce ${_tn(value)} to GritsFile`);
}

export async function coerceToResponse(value, shell) {
  if (value instanceof Result)    value = await value;
  if (value instanceof Response)  return value;
  if (value instanceof GritsFile) return value.get();
  return (await coerceToFile(value, shell)).get();
}

export async function coerceToBytes(value, shell) {
  if (value instanceof Result)      value = await value;
  if (value instanceof Uint8Array)  return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (ArrayBuffer.isView(value))    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  const buf = await (await coerceToResponse(value, shell)).arrayBuffer();
  return new Uint8Array(buf);
}

export async function coerceToText(value, shell) {
  if (value instanceof Result)      value = await value;
  if (typeof value === 'string')    return value;
  if (value instanceof ArrayBuffer) return new TextDecoder().decode(value);
  if (ArrayBuffer.isView(value))    return new TextDecoder().decode(value);
  return (await coerceToResponse(value, shell)).text();
}

export async function coerceToJS(value, shell) {
  if (value instanceof Result) value = await value;
  if (isVoid(value))  return undefined;
  if (_isPlainObject(value) || Array.isArray(value)) return value;
  if (typeof value === 'number'  ||
      typeof value === 'boolean' ||
      typeof value === 'bigint')  return value;
  return JSON.parse(await coerceToText(value, shell));
}

// ─────────────────────────────────────────────────────────────────
// Result — one pipeline stage
// ─────────────────────────────────────────────────────────────────

class Result {
  constructor(shell, promise) {
    this._shell   = shell;
    this._promise = Promise.resolve(promise);
    this._settled = null;
    this._promise.then(
      v => { this._settled = { value: v }; },
      e => { this._settled = { error: e }; }
    );
  }

  then(resolve, reject) { return this._promise.then(resolve, reject); }
  catch(reject)         { return this._promise.catch(reject); }

  toString() {
    if (!this._settled)         return 'Result(pending)';
    if (this._settled.error)    return `Result(error: ${this._settled.error.message ?? this._settled.error})`;
    const v = this._settled.value;
    if (isVoid(v))              return 'Result(void)';
    if (typeof v === 'string')  return `Result(${v.length} chars)`;
    if (v instanceof GritsFile) return `Result(GritsFile ${v.cid()})`;
    if (v instanceof GritsCID)  return `Result(GritsCID ${v.addr})`;
    if (v instanceof Path)      return `Result(${v.toString()})`;
    if (v instanceof Response)  return `Result(Response ${v.status} ${v.url || ''})`.trim();
    if (v instanceof Uint8Array || v instanceof ArrayBuffer)
      return `Result(${v.byteLength ?? v.length} bytes)`;
    return `Result(${_tn(v)})`;
  }

  async toText()     { return coerceToText    (await this._promise, this._shell); }
  async toBytes()    { return coerceToBytes   (await this._promise, this._shell); }
  async toResponse() { return coerceToResponse(await this._promise, this._shell); }
  async toFile()     { return coerceToFile    (await this._promise, this._shell); }
  async toJS()       { return coerceToJS      (await this._promise, this._shell); }

  async toCID() {
    const value = await this._promise;
    if (value instanceof GritsCID)  return value;
    if (value instanceof GritsFile) return new GritsCID(value.cid());
    if (value instanceof Path) {
      const file = await coerceToFile(value, this._shell);
      return new GritsCID(file.cid());
    }
    throw new TypeError(`Cannot coerce ${_tn(value)} to GritsCID`);
  }

  async _display() {
    const value = await this._promise;
    if (isVoid(value))              return null;
    if (typeof value === 'string')  return value;
    if (value instanceof GritsCID)  return value.addr;
    if (value instanceof Path)      return value.path;
    if (value instanceof GritsFile) return `GritsFile(${value.cid()})`;
    if (value instanceof Response) {
      try { return await value.clone().text(); }
      catch (_) { return `[Response ${value.status}]`; }
    }
    if (value instanceof Uint8Array || value instanceof ArrayBuffer)
      return `[${value.byteLength ?? value.length} bytes]`;
    if (_isPlainObject(value) || Array.isArray(value))
      return JSON.stringify(value, null, 2);
    return String(value);
  }
}

function _wrapResult(result) {
  return new Proxy(result, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target)           return Reflect.get(target, key, receiver);
      return (...args) => _wrapResult(
        new Result(target._shell,
          target._shell._dispatch(key, target, args))
      );
    },
  });
}

// ─────────────────────────────────────────────────────────────────
// GimbalShell — one per session
// ─────────────────────────────────────────────────────────────────

export class GimbalShell {
  constructor({ gg, serverUrl, volume, cwd, libUrls, evalContext = {} }) {
    this.gg           = gg;
    this.serverUrl    = serverUrl;
    this.volume       = volume;
    this.cwd          = cwd;
    this.libUrls      = libUrls ?? [];
    this._evalContext = evalContext;
    this.ui           = evalContext.ui ?? null;

    this.history      = [];
    this._importCache = new Map();
  }

  // ── Volume helpers ────────────────────────────────────────────

  _currentVol() { return this._vol(null, null); }

  _vol(serverUrl, vol) {
    return this.gg.volume(
      serverUrl ?? this.serverUrl,
      vol       ?? this.volume
    );
  }

  // ── Path parsing ──────────────────────────────────────────────
  // Parses scp-style syntax: [[host:]volume/]path
  // Returns { serverUrl, volume, path } where null means "use current".
  //
  //   'lib/grits'                   → { serverUrl: null, volume: null, path: 'lib/grits' }
  //   '/lib/grits'                  → { serverUrl: null, volume: null, path: 'lib/grits' }
  //   ':client'                     → { serverUrl: null, volume: 'client', path: '' }
  //   ':client/lib'                 → { serverUrl: null, volume: 'client', path: 'lib' }
  //   'test.melanic.org:client/lib' → { serverUrl: 'https://test.melanic.org', volume: 'client', path: 'lib' }

  _parsePath(p) {
    if (!p) return { serverUrl: null, volume: null, path: '' };

    const colonIdx = p.indexOf(':');
    if (colonIdx !== -1) {
      const hostPart  = p.slice(0, colonIdx);
      const rest      = p.slice(colonIdx + 1);
      const slashIdx  = rest.indexOf('/');
      const volume    = slashIdx === -1 ? rest : rest.slice(0, slashIdx);
      const path      = slashIdx === -1 ? ''   : rest.slice(slashIdx + 1);
      const serverUrl = hostPart
        ? (hostPart.startsWith('http') ? hostPart : `https://${hostPart}`)
        : null;
      return { serverUrl, volume: volume || null, path };
    }

    return { serverUrl: null, volume: null, path: p.replace(/^\/+/, '') };
  }

  // ── Path resolution ───────────────────────────────────────────
  // Resolves a plain path relative to cwd.
  // Cross-volume/server paths (containing ':') are returned as-is.

  resolvePath(p) {
    if (p && p.includes(':')) return p; // cross-volume — let _parsePath handle it
    if (!p || p === '.') return this.cwd;
    if (p.startsWith('/'))            return p.replace(/^\/+/, '');
    if (!this.cwd)                    return p;
    return `${this.cwd}/${p}`.replace(/\/+/g, '/');
  }

  // ── Tool import ───────────────────────────────────────────────

  async _importTool(name) {
    if (this._importCache.has(name)) return this._importCache.get(name);

    if (this.libUrls.length === 0)
      throw new Error(`command not found: ${name} (no libUrls configured)`);

    for (const base of this.libUrls) {
      const url = `${base.replace(/\/$/, '')}/${name}/main.js`;
      let mod;
      try { mod = await import(url); }
      catch (_) { continue; }

      if (typeof mod.invoke !== 'function')
        throw new Error(`${url} has no exported invoke()`);

      this._importCache.set(name, mod);
      return mod;
    }

    throw new Error(`command not found: ${name}`);
  }

  // ── Help handling ─────────────────────────────────────────────

  _isHelpCall(args) {
    return args.length === 1 && _isPlainObject(args[0]) && args[0].help === true;
  }

  // ── Dispatch ──────────────────────────────────────────────────

  async _dispatch(name, prevResult, args) {
    const mod = await this._importTool(name);
    if (this._isHelpCall(args))
      return mod.help ?? `${name}: no help text available`;
    return mod.invoke(this, prevResult, args);
  }

  // ── Built-in: cd ──────────────────────────────────────────────
  // Supports plain paths, :volume/path, and host:volume/path syntax.
  // Updates this.serverUrl and this.volume when crossing boundaries.

  async _builtinCd(path) {
    let serverUrl = this.serverUrl;
    let volume    = this.volume;
    let cwd       = this.cwd;
    let rest      = path;

    // Step 1: if ':' appears before the first '/', extract host+volume
    const slashIdx  = rest.indexOf('/');
    const colonIdx  = rest.indexOf(':');
    if (colonIdx !== -1 && (slashIdx === -1 || colonIdx < slashIdx)) {
      const hostPart = rest.slice(0, colonIdx);
      rest           = rest.slice(colonIdx + 1);            // chop "host:" prefix
      const nextSlash = rest.indexOf('/');
      const volPart  = nextSlash === -1 ? rest : rest.slice(0, nextSlash);
      rest           = nextSlash === -1 ? ''   : rest.slice(nextSlash + 1);
      if (hostPart) serverUrl = hostPart.startsWith('http') ? hostPart : `https://${hostPart}`;
      volume = volPart || volume;
      cwd    = '';                                          // reset to volume root
    }

    // Step 2: if what's left starts with '/', it's absolute within the volume
    if (rest.startsWith('/')) {
      cwd  = '';
      rest = rest.replace(/^\/+/, '');
    }

    // Step 3: resolve whatever's left relative to cwd
    if (rest) {
      cwd = cwd ? `${cwd}/${rest}` : rest;
      cwd = cwd.replace(/\/+/g, '/').replace(/\/$/, '');
    }

    const vol  = this.gg.volume(serverUrl, volume);
    const file = await vol.lo(cwd || '');
    if (!file.isDir()) throw new Error(`cd: not a directory: ${path}`);

    this.serverUrl = serverUrl;
    this.volume    = volume;
    this.cwd       = cwd;
    return VOID;
  }

  // ── Location — for prompt display ─────────────────────────────
  // Returns a concise string for the current location. The caller passes
  // the session defaults so we only show what's changed.
  //
  //   same volume, cwd='lib/grits'  → 'lib/grits'
  //   volume='client', cwd='lib'    → ':client/lib'   (defaultVolume differs)

  location({ defaultServerUrl = null, defaultVolume = null } = {}) {
    const showVolume = this.volume && this.volume !== defaultVolume;
    const path       = this.cwd || '/';
    if (showVolume) return `:${this.volume}/${path}`;
    return path;
  }

  // ── Root result (void input, start of every eval) ─────────────

  _rootResult() {
    return _wrapResult(new Result(this, Promise.resolve(VOID)));
  }

  // ── eval ──────────────────────────────────────────────────────

  async eval(src) {
    this.history.push(src);

    const root  = this._rootResult();
    const shell = this;

    const withTarget = new Proxy(Object.create(null), {
      has(_, key) {
        // Let known globals through so e.g. console.log, Math, etc. work normally.
        // Only claim keys that don't exist on globalThis.
        if (typeof key === 'symbol') return false;
        if (key in globalThis)       return false;
        return true;
      },
      get(_, key) {
        if (typeof key === 'symbol') return undefined;

        if (key === 'cd')
          return (path) => _wrapResult(
            new Result(shell, shell._builtinCd(path)));

        if (key === 'cid') return cid;

        if (key === '$')
          return (result) => {
            if (result instanceof Result) return result._promise;
            return Promise.resolve(result);
          };

        return (...args) => _wrapResult(
          new Result(shell, shell._dispatch(key, root, args)));
      },
    });

    let finalResult;
    try {
      const fn = new Function('__w__', `with (__w__) { return (async () => (${src}))(); }`);      finalResult = fn(withTarget);
    } catch (e) {
      throw new Error(`eval error: ${e.message}`);
    }

    if (!(finalResult instanceof Result)) {
      finalResult = new Result(this, Promise.resolve(finalResult));
    }

    const display = await finalResult._display();
    return { value: await finalResult, display };
  }
}

// ─────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────

export function makeShell(opts) { return new GimbalShell(opts); }