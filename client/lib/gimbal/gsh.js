// gsh.js — Gimbal Shell
//
// Architecture:
//   GimbalShell   — one per session. Holds cwd, history, lib URLs, import
//                   cache. This is what xterm.js (or any REPL host) holds.
//
//   GimbalProcess — one per pipeline stage. Wraps a Promise<value> and a ref
//                   to the shell. Thenable so tools can `await previous`.
//                   Always returned wrapped in a dispatch Proxy so that
//                   unknown method calls auto-dispatch to lib/<n>/main.js.
//
// Tool contract:
//   Each tool is a standard ES module served from a known URL. It must export:
//     export async function invoke(shell, previous, args) { ... }
//   - shell    : GimbalShell  (cwd, lib URLs, coercion helpers, vol access)
//   - previous : GimbalProcess (thenable; await it to get the prior result)
//   - args     : array of arguments the user passed
//   - returns  : any value (or Promise<value>); the shell wraps it.
//
//   Tools are loaded via the browser's native import() from libUrls, so their
//   own relative imports resolve normally. The browser module cache handles
//   deduplication — a tool at a given URL is only fetched once per page load.
//
// Tool discovery:
//   shell.libUrls is an array of base URLs searched in order, e.g.:
//     ['https://test.melanic.org/grits/v1/content/client/lib']
//   For a command named 'grep', it tries:
//     https://…/lib/grep/main.js
//   The first URL that successfully imports and exports invoke() wins.
//
// Type cascade (one direction only):
//   GritsCID → GritsFile → Response → ArrayBuffer → string → JS object
//
// Eval model:
//   shell.eval(src) evals src via new Function() (sloppy mode) with a
//   `with` Proxy so unknown identifiers dispatch to lib/<n>/main.js.
//
// Example:
//   const shell = makeShell({
//     gg, serverUrl: 'https://test.melanic.org', volume: 'client',
//     libUrls: ['https://test.melanic.org/grits/v1/content/client/lib'],
//   });
//   await shell.eval("cat('lib/grits/GritsClient.js').grep('export')")

import { GritsFile } from './GritsClient.js';

// ─────────────────────────────────────────────────────────────────
// GritsCID — explicit CID wrapper, top of the type cascade
// ─────────────────────────────────────────────────────────────────

export class GritsCID {
  constructor(addr) {
    if (typeof addr !== 'string' || !addr)
      throw new TypeError(`GritsCID: addr must be a non-empty string, got ${_tn(addr)}`);
    this.addr = addr;
  }
  toString() { return this.addr; }
}

// Convenience constructor exposed in the shell eval context as cid('Qm...')
export function cid(addr) { return new GritsCID(addr); }

// ─────────────────────────────────────────────────────────────────
// GimbalPath — unresolved path reference within a server+volume
// ─────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────
// Void sentinel — returned by commands with no meaningful output
// ─────────────────────────────────────────────────────────────────

export const VOID = Object.freeze({ _gimbalVoid: true });
export function isVoid(v) { return v === null || v === undefined || v === VOID; }

// ─────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────

function _tn(v) {
  if (v === null)      return 'null';
  if (v === undefined) return 'undefined';
  return v?.constructor?.name ?? typeof v;
}

// ─────────────────────────────────────────────────────────────────
// Coercion cascade
// GritsCID → GritsFile → Response → ArrayBuffer → string → JS object
//
// Each step accepts a GimbalProcess and awaits it first, so tools can
// pass process arguments directly without manually awaiting them.
// ─────────────────────────────────────────────────────────────────

export async function coerceToFile(value, shell) {
  if (value instanceof GimbalProcess) value = await value;
  if (value instanceof GritsFile)     return value;
  if (value instanceof GritsCID) {
    const meta = await shell._currentVol().meta(value.addr);
    return new GritsFile(value.addr, meta, shell._currentVol());
  }
  if (value instanceof GimbalPath)
    return shell._vol(value.serverUrl, value.volume).lo(value.path);
  if (typeof value === 'string')
    return shell._currentVol().lo(shell.resolvePath(value));
  throw new TypeError(`Cannot coerce ${_tn(value)} to GritsFile`);
}

export async function coerceToResponse(value, shell) {
  if (value instanceof GimbalProcess) value = await value;
  if (value instanceof Response)      return value;
  if (value instanceof GritsFile)     return value.get();
  return (await coerceToFile(value, shell)).get();
}

export async function coerceToBytes(value, shell) {
  if (value instanceof GimbalProcess) value = await value;
  if (value instanceof ArrayBuffer)   return value;
  if (ArrayBuffer.isView(value))      return value.buffer;
  return (await coerceToResponse(value, shell)).arrayBuffer();
}

export async function coerceToText(value, shell) {
  if (value instanceof GimbalProcess) value = await value;
  if (typeof value === 'string')      return value;
  if (value instanceof ArrayBuffer)   return new TextDecoder().decode(value);
  if (ArrayBuffer.isView(value))      return new TextDecoder().decode(value);
  return (await coerceToResponse(value, shell)).text();
}

export async function coerceToJS(value, shell) {
  if (value instanceof GimbalProcess) value = await value;
  if (isVoid(value)) return undefined;
  // Already a plain JS object/array — pass through
  if (typeof value === 'object'       &&
      !(value instanceof Response)    &&
      !(value instanceof ArrayBuffer) &&
      !(value instanceof GritsFile)   &&
      !(value instanceof GritsCID)    &&
      !(value instanceof GimbalPath))
    return value;
  return JSON.parse(await coerceToText(value, shell));
}

// ─────────────────────────────────────────────────────────────────
// GimbalProcess — one pipeline stage
// ─────────────────────────────────────────────────────────────────

class GimbalProcess {
  constructor(shell, promise) {
    this._shell   = shell;
    this._promise = Promise.resolve(promise);
  }

  // Thenable — so `await process` and promise chaining both work
  then(resolve, reject) { return this._promise.then(resolve, reject); }
  catch(reject)         { return this._promise.catch(reject); }

  // Produce a display string for the REPL. Returns null for void.
  async _display() {
    const value = await this._promise;
    if (isVoid(value))               return null;
    if (typeof value === 'string')   return value;
    if (value instanceof GritsCID)   return value.toString();
    if (value instanceof GimbalPath) return value.toString();
    if (value instanceof GritsFile)  return `GritsFile(${value.cid()})`;
    if (value instanceof Response) {
      try { return await value.clone().text(); }
      catch (_) { return '[Response]'; }
    }
    if (value instanceof ArrayBuffer)
      return `[ArrayBuffer ${value.byteLength} bytes]`;
    if (value?.toString && value.toString !== Object.prototype.toString)
      return value.toString();
    return JSON.stringify(value, null, 2);
  }
}

// Wrap a GimbalProcess in a Proxy that auto-dispatches unknown method names
// through the shell's lib lookup. This is what makes chaining work uniformly.
function _wrapProcess(proc) {
  return new Proxy(proc, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target)           return Reflect.get(target, key, receiver);
      // Unknown name → dispatch to lib/<key>/main.js, this process as previous
      return (...args) => _wrapProcess(
        new GimbalProcess(target._shell,
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
    // libUrls: array of base URLs to search for tool modules, in order.
    // e.g. ['https://test.melanic.org/grits/v1/content/client/lib']
    // Tool 'grep' is looked up as <base>/grep/main.js
    this.libUrls      = libUrls ?? [];
    this.history      = [];
    // Note: the browser's native module cache already deduplicates imports by
    // URL, so _importCache is just a fast-path to avoid repeated URL probing
    // across multiple libUrls entries.
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
  // Searches libUrls in order. Uses the browser's native import() so:
  //   - the browser module cache handles dedup across calls
  //   - relative imports inside tools resolve correctly against their URL
  //   - CORS and caching headers are respected automatically

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

  // ── Dispatch a tool call ──────────────────────────────────────

  async _dispatch(name, prevProcess, args) {
    const mod = await this._importTool(name);
    return mod.invoke(this, prevProcess, args);
  }

  // ── Built-in: cd ──────────────────────────────────────────────

  async _builtinCd(path) {
    const resolved = this.resolvePath(path);
    const file     = await this._currentVol().lo(resolved || '.');
    if (!file.isDir()) throw new Error(`cd: not a directory: ${path}`);
    this.cwd = resolved;
    return VOID;
  }

  // ── Root process (void input, start of every eval) ────────────

  _rootProcess() {
    return _wrapProcess(new GimbalProcess(this, Promise.resolve(VOID)));
  }

  // ── eval — main REPL entry point ──────────────────────────────
  //
  // Evaluates `src` as a JS expression in sloppy mode (via new Function so
  // that `with` works). Unknown identifiers in the expression are intercepted
  // by the withTarget Proxy and dispatched to lib/<n>/main.js.
  //
  // Returns { value, display } where display is the string to show the user,
  // or null if the result was void.

  async eval(src) {
    this.history.push(src);

    const root  = this._rootProcess();
    const shell = this;

    const withTarget = new Proxy(Object.create(null), {
      has()       { return true; }, // claim everything so `with` routes here
      get(_, key) {
        if (typeof key === 'symbol') return undefined;

        // Built-ins wired directly, bypassing lib lookup
        if (key === 'cd')
          return (path) => _wrapProcess(
            new GimbalProcess(shell, shell._builtinCd(path)));

        // cid() convenience constructor
        if (key === 'cid') return cid;

        // Everything else → dispatch through lib URL search.
        // Root process (void) is the initial previous for the first
        // command on the line; subsequent commands in the chain are
        // dispatched by the _wrapProcess Proxy on the returned process.
        return (...args) => _wrapProcess(
          new GimbalProcess(shell, shell._dispatch(key, root, args)));
      },
    });

    let finalProcess;
    try {
      // new Function always produces sloppy-mode code, enabling `with`.
      const fn = new Function('__w__', `with (__w__) { return (${src}); }`);
      finalProcess = fn(withTarget);
    } catch (e) {
      throw new Error(`eval error: ${e.message}`);
    }

    // If the expression returned a plain value rather than a process, wrap it
    // so the rest of the machinery has a uniform interface.
    if (!(finalProcess instanceof GimbalProcess)) {
      finalProcess = new GimbalProcess(this, Promise.resolve(finalProcess));
    }

    const display = await finalProcess._display();
    return { value: await finalProcess, display };
  }
}

// ─────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────

export function makeShell(opts) { return new GimbalShell(opts); }