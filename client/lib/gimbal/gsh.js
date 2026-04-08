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
export function isVoid(v) { return v === null || v === undefined || v === VOID; } // FIXME

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
// Response constructors — for tools to use explicitly
// ─────────────────────────────────────────────────────────────────

export function responseFromText(text) {
  return new Response(new TextEncoder().encode(String(text)), {
    status:  200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
}

export function responseFromBytes(bytes) {
  return new Response(bytes, { status: 200 });
}

export function responseFromJSON(obj) {
  return new Response(JSON.stringify(obj), {
    status:  200,
    headers: { 'Content-Type': 'application/json' },
  });
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
    if (!this._settled)      return 'Result(pending)';
    if (this._settled.error) return `Result(error: ${this._settled.error.message ?? this._settled.error})`;
    const v = this._settled.value;
    if (isVoid(v))           return 'Result(void)';
    if (v instanceof Response)
      return `Result(Response ${v.status} ${v.url || ''})`.trim();
    if (v instanceof GritsFile)
      return `Result(GritsFile ${v.cid()})`;
    if (typeof v === 'string')
      return v.length > 20 ? `Result("${v.slice(0, 20)}…")` : `Result("${v}")`;
    if (v instanceof Uint8Array || v instanceof ArrayBuffer)
      return `Result(${v.byteLength ?? v.length} bytes)`;
    return `Result(${_tn(v)})`;
  }

  // Terminal methods — explicit exit from pipeline world into JS world.
  // These expect the pipeline value to be a Response.

  async toText() {
    const v = await this._promise;
    if (isVoid(v)) return '';
    if (!(v instanceof Response))
      throw new TypeError(`toText: expected Response in pipeline, got ${_tn(v)}`);
    return v.clone().text();
  }

  async toBytes() {
    const v = await this._promise;
    if (isVoid(v)) return new Uint8Array(0);
    if (!(v instanceof Response))
      throw new TypeError(`toBytes: expected Response in pipeline, got ${_tn(v)}`);
    return new Uint8Array(await v.clone().arrayBuffer());
  }

  async toJS() {
    const v = await this._promise;
    if (isVoid(v)) return null;
    if (!(v instanceof Response))
      throw new TypeError(`toJS: expected Response in pipeline, got ${_tn(v)}`);
    return v.clone().json();
  }

  async toResponse() {
    const v = await this._promise;
    if (isVoid(v)) return null;
    if (!(v instanceof Response))
      throw new TypeError(`toResponse: expected Response in pipeline, got ${_tn(v)}`);
    return v;
  }

  async toFile() {
    const v = await this._promise;
    if (v instanceof GritsFile) return v;
    throw new TypeError(`toFile: expected GritsFile, got ${_tn(v)}`);
  }
}

function _dispatchWrapped(shell, name, prevResult, args, historyIndex) {
  const promise = shell._dispatch(name, prevResult, args).then(v => {
    const result = new Result(shell, Promise.resolve(v));
    const wrapped = _wrapResult(result, historyIndex);
    if (historyIndex !== null) shell.__[historyIndex] = wrapped;
    return v;
  });
  return _wrapResult(new Result(shell, promise), historyIndex);
}

function _wrapResult(result, historyIndex) {
  return new Proxy(result, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target)           return Reflect.get(target, key, receiver);
      return (...args) => _dispatchWrapped(target._shell, key, target, args, historyIndex);
    }
  });
}

// ─────────────────────────────────────────────────────────────────
// GimbalShell — one per session
// ─────────────────────────────────────────────────────────────────

export class GimbalShell {
  constructor({ fs, serverUrl, volume, cwd, libs, evalContext = {} }) {
    this.fs              = fs;
    this.serverUrl       = serverUrl;
    this.volume          = volume;
    this.cwd             = cwd || '/';
    this.libs            = libs ?? [];
    this._evalContext    = evalContext;
    this.history         = [];
    this.__              = [];
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
    return this.fs.volume(
      serverUrl ?? this.serverUrl,
      vol       ?? this.volume
    );
  }

  // ── Path resolution ───────────────────────────────────────────
  // Returns { serverUrl, volume, path } always.
  // Callers that previously did shell._currentVol().lookup(shell.resolvePath(p))
  // should now do shell._vol(r.serverUrl, r.volume).lookup(r.path).
  resolvePath(p) {
    if (!p || p === '.') {
      return {
        serverUrl: this.serverUrl,
        volume:    this.volume,
        path:      this.cwd.replace(/^\//, '') || '',
      };
    }

    // scp-style cross-volume: [server]:volume[/path]
    if (p.includes(':')) {
      const colonIdx  = p.indexOf(':');
      const serverUrl = p.slice(0, colonIdx) || this.serverUrl;
      const rest      = p.slice(colonIdx + 1);
      const slashIdx  = rest.indexOf('/');
      const volume    = slashIdx === -1 ? rest            : rest.slice(0, slashIdx);
      const path      = slashIdx === -1 ? ''              : rest.slice(slashIdx + 1);
      return { serverUrl, volume, path };
    }

    // Absolute path in current volume.
    if (p.startsWith('/')) {
      return { serverUrl: this.serverUrl, volume: this.volume, path: p.replace(/^\/+/, '') };
    }

    // Relative path — join with cwd.
    const base = this.cwd.replace(/^\/+|\/+$/g, '');
    const joined = base ? `${base}/${p}` : p;
    return { serverUrl: this.serverUrl, volume: this.volume, path: joined };
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
      catch (e) { console.error(`_importTool: failed to import ${url}:`, e); continue; }
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
    this._cacheWarmed    = true;
    this._availableTools = new Set();
    for (const lib of this.libs) {
      try {
        const vol  = this.fs.volume(lib.serverUrl, lib.volume);
        const file = await vol.lookup(lib.path);
        if (!file.isDir()) continue;
        this._libDirCIDs.set(_libKey(lib), file.cid());
        const dir = await file.json();
        for (const name of Object.keys(dir)) this._availableTools.add(name);
      } catch (e) { console.error('warmCache failed for lib:', lib, e); }
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
        const vol  = this.fs.volume(lib.serverUrl, lib.volume);
        const info = await vol._tryFastLookup(lib.path);
        if (!info) continue;
        const known = this._libDirCIDs.get(_libKey(lib));
        if (known && info.metadataHash !== known) { stale = true; break; }
      } catch (e) {}
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

  _rootResult(initial, historyIndex) {
    return _wrapResult(new Result(this, Promise.resolve(initial)), historyIndex);
  }

  // ── eval ──────────────────────────────────────────────────────

  async eval(src, extraVars = {}, { doHistory = false } = {}) {
    await this._warmCache();
    await this._checkCacheStale();
    this.history.push(src);

    const __ = this.__;
    const _  = __.length ? __[__.length - 1] : VOID;
    const historyIndex = doHistory ? __.length : null;
    if (doHistory)
      __.push(undefined);

    const root  = this._rootResult(VOID, historyIndex);
    const shell = this;

    const withTarget = new Proxy(Object.create(null), {
      has(_, key) {
        if (typeof key === 'symbol') return false;
        if (key === '__')            return true;
        if (key === '_')             return true;
        if (key in globalThis)       return false;
        if (key in extraVars)        return true;
        return shell._availableTools?.has(key) ?? false;
      },
      get(_, key) {
        if (typeof key === 'symbol') return undefined;
        if (key === '__') return __;
        if (key === '_')  return _;
        if (key in extraVars)        return extraVars[key];
        return (...args) => _dispatchWrapped(shell, key, root, args, historyIndex);
      },
    });

    let finalResult;
    try {
      const fn = new Function('__w__', `with (__w__) { return (async () => (${src}))(); }`);
      finalResult = await fn(withTarget);
    } catch (e) {
      throw new Error(`eval error: ${e.message}`);
    }

    if (doHistory && __[historyIndex] === undefined) {
      __[historyIndex] = finalResult;
    }

    return finalResult;
  }
}

function _libKey(lib) { return `${lib.serverUrl}|${lib.volume}|${lib.path}`; }

export function makeShell(opts) { return new GimbalShell(opts); }