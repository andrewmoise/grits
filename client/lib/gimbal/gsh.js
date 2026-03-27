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
// Eval model:
//   shell.eval(src) evals src via new Function() (sloppy mode) with a
//   `with` Proxy so unknown identifiers dispatch to lib/<n>/main.js.
//   Special names wired into the eval context:
//     cd(path)    — built-in, updates cwd, returns void Result
//     cid(addr)   — constructs a GritsCID
//     $(result)   — unwraps a Result to its raw Promise<value>
//
// Example:
//   const shell = makeShell({
//     gg, serverUrl: 'https://test.melanic.org', volume: 'client',
//     libUrls: ['https://test.melanic.org/grits/v1/content/client/lib'],
//   });
//   await shell.eval("cat('lib/grits/GritsClient.js').grep('export')")
//
//   // Outside eval, use terminal methods:
//   const text = await shell.cat('lib/grits/GritsClient.js').toText()

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
  valueOf()  { return this.addr; } // so == comparisons work naturally
}

// Convenience constructor, available as cid('Qm...') inside shell.eval()
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

function _isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

// ─────────────────────────────────────────────────────────────────
// Coercion cascade
// GritsCID → GritsFile → Response → ArrayBuffer → string → JS object
//
// Each step accepts a Result and awaits it first, so tools can pass
// process arguments directly without manually awaiting.
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
  if (value instanceof Result)   value = await value;
  if (value instanceof Response) return value;
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
    // Cache the resolved value so toString() can be informative after settling
    this._settled = null; // { value } | { error }
    this._promise.then(
      v  => { this._settled = { value: v }; },
      e  => { this._settled = { error: e }; }
    );
  }

  // ── Thenable ──────────────────────────────────────────────────
  then(resolve, reject) { return this._promise.then(resolve, reject); }
  catch(reject)         { return this._promise.catch(reject); }

  // ── Non-blocking description, safe in any string context ──────
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

  // ── Terminal methods — cross the boundary to plain JS values ──

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

  // ── REPL display ──────────────────────────────────────────────
  // Used by shell.eval() to produce the string shown to the user.
  // May do I/O (reads Response body), unlike toString().

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

// Wrap a Result in a Proxy that auto-dispatches unknown method names.
// This is what makes chaining work uniformly at every stage.
function _wrapResult(result) {
  return new Proxy(result, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target)           return Reflect.get(target, key, receiver);
      // Unknown name → dispatch to lib/<key>/main.js with this result as previous
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
  constructor({ gg, serverUrl = null, volume = null, cwd = '', libUrls = null }) {
    this.gg           = gg;
    this.serverUrl    = serverUrl;
    this.volume       = volume;
    this.cwd          = cwd;
    this.libUrls      = libUrls ?? [];
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

  // ── Path resolution ───────────────────────────────────────────

  resolvePath(p) {
    if (!p || p === '.' || p === '/') return this.cwd;
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
      console.log("Trying URL: ", url);
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

    if (this._isHelpCall(args)) {
      return mod.help ?? `${name}: no help text available`;
    }

    return mod.invoke(this, prevResult, args);
  }

  // ── Built-in: cd ──────────────────────────────────────────────

  async _builtinCd(path) {
    const resolved = this.resolvePath(path);
    const file     = await this._currentVol().lo(resolved || '.');
    if (!file.isDir()) throw new Error(`cd: not a directory: ${path}`);
    this.cwd = resolved;
    return VOID;
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
      has()       { return true; },
      get(_, key) {
        if (typeof key === 'symbol') return undefined;

        // Built-ins
        if (key === 'cd')
          return (path) => _wrapResult(
            new Result(shell, shell._builtinCd(path)));

        // cid() convenience constructor
        if (key === 'cid') return cid;

        // $ — unwrap a Result to its raw Promise<value>
        if (key === '$')
          return (result) => {
            if (result instanceof Result) return result._promise;
            return Promise.resolve(result);
          };

        // Everything else → dispatch through lib URL search.
        // Root result (void) is previous for the first command on the line.
        return (...args) => _wrapResult(
          new Result(shell, shell._dispatch(key, root, args)));
      },
    });

    let finalResult;
    try {
      const fn = new Function('__w__', `with (__w__) { return (${src}); }`);
      finalResult = fn(withTarget);
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