// gsh.js — Gimbal Shell
//
// Architecture:
//   GimbalShell   — one per session. Holds cwd, history, import cache, lib
//                   paths. This is what xterm.js (or any REPL host) holds.
//
//   GimbalProcess — one per pipeline stage. Wraps a Promise<value> and a ref
//                   to the shell. Thenable so tools can `await previous`.
//                   Always returned wrapped in a dispatch Proxy so that
//                   unknown method calls auto-dispatch to lib/<name>/main.js.
//
// Tool contract:
//   lib/<name>/main.js must export:
//     export async function invoke(shell, previous, args) { ... }
//   - shell    : GimbalShell  (cwd, lib paths, helpers)
//   - previous : GimbalProcess (thenable; await it to get the prior result)
//   - args     : array of arguments the user passed
//   - returns  : any value (or Promise<value>); the shell wraps it.
//
// Coercion cascade (one direction only):
//   GritsFile → CID string → GimbalStream → bytes → string → JS object
//   Each tool is explicit about what it accepts and coerces forward as needed.
//
// Eval model:
//   shell.eval(src) evals src in sloppy mode using an indirect eval so that
//   `with` works. Unknown identifiers are caught by a Proxy and dispatched.
//
// Example:
//   await shell.eval("cat('GritsClient.js').grep('FOR MODULE').li('out.txt')")

import { GritsFile, GritsVolume } from './GritsClient.js'; // %FOR MODULE%
//const { GritsFile, GritsVolume } = self.Grits;           // %FOR SERVICEWORKER%

// ─────────────────────────────────────────────────────────────────
// Shell-layer I/O types
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
    this.source      = source;
  }
  get response() { return this._response; }
  async bytes()  { return this._response.arrayBuffer(); }
  async text()   { return this._response.text(); }
  async json()   { return this._response.json(); }
  toString()     { return `[GimbalStream type=${this.contentType} size=${this.size}]`; }
}

// Void sentinel — returned by commands that produce no output (cd, mkdir, …)
export const VOID = Object.freeze({ _gimbalVoid: true });

export function isVoid(v) {
  return v === null || v === undefined || v === VOID;
}

export { coerceToFile, coerceToStream, coerceToJS };

// ─────────────────────────────────────────────────────────────────
// Coercion helpers
// ─────────────────────────────────────────────────────────────────

async function coerceToFile(value, shell) {
  if (value instanceof GritsFile) return value;
  if (value instanceof GimbalPath) {
    const vol = shell._vol(value.serverUrl, value.volume);
    return vol.lo(value.path);
  }
  if (typeof value === 'string') {
    // Treat as a metadata CID — ask the current volume for its meta
    const meta = await shell._currentVol().meta(value);
    return new GritsFile(value, meta, shell._currentVol());
  }
  throw new TypeError(`Cannot coerce ${_tn(value)} to GritsFile`);
}

async function coerceToStream(value, shell) {
  if (value instanceof GimbalStream) return value;
  if (value instanceof GritsFile)
    return new GimbalStream(await value.get(), { source: value });
  if (value instanceof GimbalPath || typeof value === 'string') {
    const file = await coerceToFile(value, shell);
    return new GimbalStream(await file.get(), { source: file });
  }
  throw new TypeError(`Cannot coerce ${_tn(value)} to GimbalStream`);
}

async function coerceToJS(value, shell) {
  if (isVoid(value))             return undefined;
  if (value instanceof GimbalStream) return value.json();
  if (value instanceof GritsFile)    return value.json();
  if (value instanceof GimbalPath || typeof value === 'string')
    return (await coerceToStream(value, shell)).json();
  return value;
}

function _tn(v) {
  if (v === null)      return 'null';
  if (v === undefined) return 'undefined';
  return v?.constructor?.name ?? typeof v;
}

// ─────────────────────────────────────────────────────────────────
// Module loading from source string (blob URL trick)
// ─────────────────────────────────────────────────────────────────

async function _evalModule(src) {
  const blob = new Blob([src], { type: 'text/javascript' });
  const url  = URL.createObjectURL(blob);
  try    { return await import(url); }
  finally { URL.revokeObjectURL(url); }
}

// ─────────────────────────────────────────────────────────────────
// GimbalProcess — one pipeline stage
// ─────────────────────────────────────────────────────────────────

class GimbalProcess {
  // _promise: Promise<any>  — resolves to this stage's output value
  constructor(shell, promise) {
    this._shell   = shell;
    this._promise = Promise.resolve(promise);
  }

  // Thenable — so tools can `await previous` and chain .then()
  then(resolve, reject) { return this._promise.then(resolve, reject); }
  catch(reject)         { return this._promise.catch(reject); }

  // Unwrap to a display string for the REPL
  async _display() {
    const value = await this._promise;
    if (isVoid(value)) return null;
    if (typeof value === 'string') return value;
    if (value?.toString && value.toString !== Object.prototype.toString)
      return value.toString();
    return JSON.stringify(value, null, 2);
  }
}

// Wrap a GimbalProcess in a Proxy that auto-dispatches unknown method names
// through the shell's lib lookup.
function _wrapProcess(proc) {
  return new Proxy(proc, {
    get(target, key, receiver) {
      // Pass through known GimbalProcess props / Promise protocol
      if (key in target || typeof key === 'symbol') {
        return Reflect.get(target, key, receiver);
      }
      // Unknown name → return a function that dispatches to lib/<key>/main.js
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
  constructor({ gg, serverUrl = null, volume = null, cwd = '', libPaths = null }) {
    this.gg        = gg;                           // GritsClient instance
    this.serverUrl = serverUrl;
    this.volume    = volume;
    this.cwd       = cwd;
    this.libPaths  = libPaths ?? ['lib'];          // searched in order
    this.history   = [];
    this._importCache = new Map();                 // name → module exports
  }

  // ── Volume helpers ────────────────────────────────────────────

  _currentVol() {
    return this._vol(this.serverUrl, this.volume);
  }

  _vol(serverUrl, volume) {
    return this.gg.volume(
      serverUrl ?? this.serverUrl,
      volume    ?? this.volume
    );
  }

  // ── Path helpers ──────────────────────────────────────────────

  resolvePath(p) {
    if (!p || p === '.') return this.cwd;
    if (p.startsWith('/')) return p.replace(/^\/+/, '');
    if (!this.cwd) return p;
    return `${this.cwd}/${p}`.replace(/\/+/g, '/');
  }

  // ── Import a tool module, with caching ───────────────────────

  async _importTool(name) {
    if (this._importCache.has(name)) return this._importCache.get(name);

    const vol = this._currentVol();
    for (const base of this.libPaths) {
      const tryPath = `${base}/${name}/main.js`;
      let file;
      try { file = await vol.lo(tryPath); }
      catch (_) { continue; }

      const resp = await file.get();
      if (!resp.ok) continue;

      const mod = await _evalModule(await resp.text());
      if (typeof mod.invoke !== 'function')
        throw new Error(`lib/${name}/main.js has no exported invoke()`);

      this._importCache.set(name, mod);
      return mod;
    }

    throw new Error(`command not found: ${name}`);
  }

  // ── Dispatch: look up tool, call invoke(), return Promise ─────

  async _dispatch(name, prevProcess, args) {
    const mod    = await this._importTool(name);
    const result = await mod.invoke(this, prevProcess, args);
    return result;
  }

  // ── Built-in commands (available without a lib/ lookup) ───────

  // cd — changes cwd, returns VOID
  async _builtinCd(path) {
    const resolved = this.resolvePath(path);
    // Verify the path exists and is a directory
    const file = await this._currentVol().lo(resolved || '.');
    if (!file.isDir()) throw new Error(`cd: not a directory: ${path}`);
    this.cwd = resolved;
    return VOID;
  }

  // ── Root process factory ──────────────────────────────────────

  _rootProcess() {
    return _wrapProcess(new GimbalProcess(this, Promise.resolve(VOID)));
  }

  // ── eval — the main REPL entry point ─────────────────────────
  //
  // Evals `src` in sloppy mode (so `with` works) with the root process as
  // the implicit receiver for unknown identifiers.
  //
  // Returns { value, display } where display is the string to print (or null
  // for void results).

  async eval(src) {
    this.history.push(src);

    const root    = this._rootProcess();
    const shell   = this;

    // The `with`-target Proxy: known properties pass through to `root`,
    // unknown ones return a dispatch function bound to `root`.
    const withTarget = new Proxy(root, {
      has(_t, _key)  { return true; },   // claim everything so `with` always asks us
      get(target, key) {
        // Let JS internals and known process props through
        if (typeof key === 'symbol') return Reflect.get(target, key);

        // Built-ins wired directly to the shell
        if (key === 'cd') return (path) => _wrapProcess(
          new GimbalProcess(shell, shell._builtinCd(path)));

        // Known GimbalProcess properties (then, catch, _shell, …)
        if (key in target) return Reflect.get(target, key);

        // Unknown → dispatch through lib lookup, starting from root (void) input
        return (...args) => _wrapProcess(
          new GimbalProcess(shell, shell._dispatch(key, root, args)));
      },
    });

    // Indirect eval runs in global (sloppy) scope, enabling `with`.
    // We pass withTarget in via a closure so the evaled code can reach it.
    let finalProcess;
    try {
      // The IIFE wrapper lets us inject `withTarget` without polluting globals.
      // eslint-disable-next-line no-new-func
      const fn = new Function('__w__', `with (__w__) { return (${src}); }`);
      finalProcess = fn(withTarget);
    } catch (e) {
      throw new Error(`eval error: ${e.message}`);
    }

    // If the expression didn't produce a GimbalProcess (e.g. a bare value),
    // wrap it so we have a uniform interface.
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

export function makeShell(opts) {
  return new GimbalShell(opts);
}