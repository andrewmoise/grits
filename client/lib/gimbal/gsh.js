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
//                   .toJS()       → Promise<any>
//
//                 .toString() returns a non-blocking descriptive string,
//                 safe for use in string contexts without triggering I/O.
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
//   GritsFile → Response → string
//
// Path syntax (scp-style):
//   'lib/grits'                     — relative path, current server+volume
//   '/lib/grits'                    — absolute path, current server+volume
//   ':client/lib'                   — different volume, same server
//   ':client'                       — different volume root, same server
//   'test.melanic.org:client/lib'   — different server+volume
//
// Eval model:
//   shell.eval(src) evals src via new Function() (async, sloppy mode) with
//   a `with` Proxy so unknown identifiers dispatch to lib/<n>/main.js.
//   All tool names including cd, from, to, ls, cat, grep etc. are just
//   tool modules — there are no special built-in names.

import { GritsFile } from '../grits/GritsClient.js';
import stringify from '../vendor/json-stringify-pretty-compact/index.js';

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
// Coercion utilities — imported by tool modules as needed
// GritsFile → Response → ArrayBuffer → string
// ─────────────────────────────────────────────────────────────────

export async function coerceToFile(value, shell) {
  if (value instanceof Result) value = await value;
  if (value instanceof GritsFile) return value;
  if (typeof value === 'string')
    return shell._currentVol().lookup(shell.resolvePath(value).replace(/^\//, ''));
  throw new TypeError(`cannot coerce ${_tn(value)} to GritsFile — expected a path string or GritsFile`);
}

export async function coerceToResponse(value, shell) {
  if (value instanceof Result)    value = await value;
  if (value instanceof Response)  return value;
  if (value instanceof GritsFile) return value.get();
  throw new TypeError(`cannot coerce ${_tn(value)} to Response`);
}

export async function coerceToBytes(value, shell) {
  if (value instanceof Result)      value = await value;
  if (value instanceof Uint8Array)  return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (ArrayBuffer.isView(value))    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  if (_isPlainObject(value) || Array.isArray(value))
    return new TextEncoder().encode(stringify(value, { maxLength: 76 }));
  const buf = await (await coerceToResponse(value, shell)).arrayBuffer();
  return new Uint8Array(buf);
}

export async function coerceToText(value, shell) {
  if (value instanceof Result)      value = await value;
  if (typeof value === 'string')    return value;
  if (value instanceof ArrayBuffer) return new TextDecoder().decode(value);
  if (ArrayBuffer.isView(value))    return new TextDecoder().decode(value);
  if (_isPlainObject(value) || Array.isArray(value))
    return stringify(value, { maxLength: 76 });
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
    if (typeof v === 'string')
      return v.length > 20
        ? `Result("${v.slice(0, 20)}…")`
        : `Result("${v}")`;
    if (v instanceof GritsFile) return `Result(GritsFile ${v.cid()})`;
    if (v instanceof Response)  return `Result(Response ${v.status} ${v.url || ''})`.trim();
    if (v instanceof Uint8Array || v instanceof ArrayBuffer)
      return `Result(${v.byteLength ?? v.length} bytes)`;
    return `Result(${_tn(v)})`;
  }

  async toText()     { return coerceToText    (await this._promise, this._shell); }
  async toBytes()    { return coerceToBytes   (await this._promise, this._shell); }
  async toResponse() { return coerceToResponse(await this._promise, this._shell); }

  async toJS()       { return coerceToJS      (await this._promise, this._shell); }
  async toFile() {
    const value = await this._promise;
    if (value instanceof GritsFile) return value;
    throw new TypeError(`cannot coerce ${_tn(value)} to GritsFile`);
  }

  async _display(cols = 80) {
    const value = await this._promise;
    if (isVoid(value))              return null;
    if (typeof value === 'string')  return value;
    if (value instanceof GritsFile) return `GritsFile(${value.cid()})`;
    if (value instanceof Response) {
      try { return await value.clone().text(); }
      catch (_) { return `[Response ${value.status}]`; }
    }
    if (value instanceof Uint8Array || value instanceof ArrayBuffer)
      return `[${value.byteLength ?? value.length} bytes]`;
    if (_isPlainObject(value) || Array.isArray(value))
      return stringify(value, cols);
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
  constructor({ gg, serverUrl, volume, cwd, libs, evalContext = {} }) {
    this.gg           = gg;
    this.serverUrl    = serverUrl;
    this.volume       = volume;
    this.cwd          = cwd || '/';
    this.libs         = libs ?? [];
    this._evalContext = evalContext;
    this.history      = [];
    this._importCache    = new Map();
    this._availableTools = null;
    this._cacheWarmed    = false;

    // Per-lib directory CID tracking for cache invalidation.
    // Key: "<serverUrl>|<volume>|<path>", value: metadataCID string of the
    // lib directory at last warm. If this changes we bust and re-warm.
    this._libDirCIDs = new Map();
  }

  // ── ui accessor — reads evalContext live so timing doesn't matter ─

  get ui() { return this._evalContext.ui ?? null; }

  // ── Volume helpers ────────────────────────────────────────────

  _currentVol() { return this._vol(null, null); }

  _vol(serverUrl, vol) {
    return this.gg.volume(
      serverUrl ?? this.serverUrl,
      vol       ?? this.volume
    );
  }

  // ── Path resolution ───────────────────────────────────────────
  // Always returns a /‑prefixed absolute path.
  // Cross-volume paths (containing ':') are returned as-is for
  // the tool to handle via vol() directly.

  resolvePath(p) {
    if (p && p.includes(':')) return p;
    if (!p || p === '.')      return this.cwd || '/';
    if (p.startsWith('/'))    return p;
    if (!this.cwd || this.cwd === '/') return '/' + p;
    return `${this.cwd}/${p}`.replace(/\/+/g, '/');
  }

  // ── Tool import ───────────────────────────────────────────────

  _libUrl({ serverUrl, volume, path }) {
    return `${serverUrl}/grits/v1/content/${volume}/${path}`;
  }

  async _importTool(name) {
    if (this._importCache.has(name)) return this._importCache.get(name);

    for (const lib of this.libs) {
      const url = `${this._libUrl(lib)}/${name}/main.js`;
      let mod;
      try { mod = await import(url); }
      catch (e) {
        console.error(`_importTool: failed to import ${url}:`, e);
        continue;
      }

      if (typeof mod.invoke !== 'function')
        throw new Error(`${url} has no exported invoke()`);

      this._importCache.set(name, mod);
      return mod;
    }

    throw new Error(`command not found: ${name}`);
  }

  // ── Cache warming ─────────────────────────────────────────────
  // Fetches each lib directory listing to populate _availableTools
  // and records each lib dir's CID for later staleness checks.

  async _warmCache() {
    if (this._cacheWarmed) return;
    this._cacheWarmed = true;
    this._availableTools = new Set();

    for (const lib of this.libs) {
      try {
        const vol  = this.gg.volume(lib.serverUrl, lib.volume);
        const file = await vol.lookup(lib.path);
        if (!file.isDir()) continue;

        // Record this lib dir's CID so we can detect changes later
        const key = _libKey(lib);
        this._libDirCIDs.set(key, file.cid());

        const dir = await file.json();
        for (const name of Object.keys(dir)) {
          this._availableTools.add(name);
        }
      } catch (e) {
        console.error('warmCache failed for lib:', lib, e);
      }
    }
  }

  // ── Staleness check ───────────────────────────────────────────
  // Called before each eval. Uses _tryFastLookup (in-memory only,
  // no network) to compare current lib dir CIDs against what we saw
  // at warm time. If any lib dir changed, bust the entire tool cache
  // and re-warm. This is intentionally coarse — a single changed lib
  // dir invalidates everything — because tool additions/deletions are
  // entangled with config and it's cheaper to just re-warm than to
  // try to surgically update.

  async _checkCacheStale() {
    let stale = false;

    for (const lib of this.libs) {
      try {
        const vol  = this.gg.volume(lib.serverUrl, lib.volume);
        // _tryFastLookup reads only from in-memory JSON cache — no network
        const info = await vol._tryFastLookup(lib.path);
        if (!info) continue; // not in memory, can't compare — skip

        const key   = _libKey(lib);
        const known = this._libDirCIDs.get(key);
        if (known && info.metadataHash !== known) {
          stale = true;
          break;
        }
      } catch (e) {
        // best-effort — a failed check is not an error
      }
    }

    if (stale) {
      this._cacheWarmed    = false;
      this._availableTools = null;
      this._importCache.clear();
      this._libDirCIDs.clear();
      await this._warmCache();
    }
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

  // ── Root result (void input, start of every eval) ─────────────

  _rootResult() {
    return _wrapResult(new Result(this, Promise.resolve(VOID)));
  }

  // ── eval ──────────────────────────────────────────────────────

  async eval(src, cols = 80, extraVars = {}) {
    await this._warmCache();
    await this._checkCacheStale();
    this.history.push(src);

    const root  = this._rootResult();
    const shell = this;

    const withTarget = new Proxy(Object.create(null), {
      has(_, key) {
        if (typeof key === 'symbol')    return false;
        if (key in globalThis)          return false;
        if (key in extraVars)           return true;
        return shell._availableTools?.has(key) ?? false;
      },
      get(_, key) {
        if (typeof key === 'symbol') return undefined;
        if (key in extraVars)        return extraVars[key];
        return (...args) => _wrapResult(
          new Result(shell, shell._dispatch(key, root, args)));
      },
    });

    let finalResult;
    try {
      const fn = new Function('__w__', `with (__w__) { return (async () => (${src}))(); }`);
      finalResult = fn(withTarget);
    } catch (e) {
      throw new Error(`eval error: ${e.message}`);
    }

    if (!(finalResult instanceof Result)) {
      finalResult = new Result(this, Promise.resolve(finalResult));
    }

    const display = await finalResult._display(cols);
    return { value: await finalResult, display };
  }
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

function _libKey(lib) {
  return `${lib.serverUrl}|${lib.volume}|${lib.path}`;
}

// ─────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────

export function makeShell(opts) { return new GimbalShell(opts); }