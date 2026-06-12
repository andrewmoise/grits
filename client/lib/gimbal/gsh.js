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
//   For command 'grep': tries <base>/grep/main.js, first match wins.
//   The browser's native import() handles caching and relative imports inside
//   tool modules automatically.
//
// Path syntax:
//   'lib/grits'                       — relative path, current server+volume
//   '/lib/grits'                      — absolute path, current server+volume
//   '//client/lib'                    — different volume, same server
//   '//client'                        — root of different volume, same server
//   '//test.melanic.org:client/lib'   — different server+volume
//
// Eval model:
//   shell.eval(src) evals src via new Function() (async, sloppy mode) with
//   a `with` Proxy so unknown identifiers dispatch to lib/<n>/main.js.
//   All tool names including cd, from, to, ls, cat, grep etc. are just
//   tool modules — there are no special built-in names.

import { GritsFile } from '../grits/GritsClient.js';
import { glob } from './glob.js';
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
  constructor(shell, parentShell, promise) {
    this._shell   = shell;
    this._parent  = parentShell;

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

const HISTORY_NEW = Symbol('HISTORY_NEW');

function _dispatchWrapped(shell, name, prevResult, args, historyIndex) {
  const parentShell = prevResult._parent;
  if (historyIndex === HISTORY_NEW) {
    historyIndex = parentShell.__.length;
    parentShell.__.push(undefined);
  }
  const promise = shell._dispatch(name, prevResult, args).then(v => {
    const result = new Result(shell, parentShell, Promise.resolve(v));
    if (historyIndex !== null)
      parentShell.__[historyIndex] = _wrapResult(result, HISTORY_NEW)
    return v;
  });
  return _wrapResult(new Result(shell, parentShell, promise), historyIndex);
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
  constructor({ fs, serverUrl, volume, cwd, evalContext = {} }) {
    this.fs              = fs;
    this.serverUrl       = serverUrl;
    this.volume          = volume;
    this.cwd             = cwd || '/';
    this._evalContext    = evalContext;
    this._scriptScope    = Object.create(null);
    this.history         = [];
    this.__              = [];
    this._importCache    = new Map();

    // Expose direct command calls: shell.<cmd>()
    return new Proxy(this, {
      get: (target, key, receiver) => {
        if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
        if (key in target) return Reflect.get(target, key, receiver);
        return (...args) => target.runCommand(key, args);
      }
    });
  }

  // ── fork ──────────────────────────────────────────────────────
  // Create a child execution shell inheriting location + caches.
  // evalContext is shallow-cloned and rebound to the child shell.
  fork() {
    const childEvalContext = { ...(this._evalContext || {}) };

    const child = new GimbalShell({
      fs: this.fs,
      serverUrl: this.serverUrl,
      volume: this.volume,
      cwd: this.cwd,
      evalContext: childEvalContext,
    });

    // Share import cache for performance
    child._importCache = this._importCache;

    // Rebind evalContext.shell to child
    childEvalContext.shell = child;

    return child;
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
  // Returns { serverUrl, volume, path, trailingSlash } always.
  // Callers that previously did shell._currentVol().lookup(shell.resolvePath(p))
  // should now do shell._vol(r.serverUrl, r.volume).lookup(r.path).
  resolvePath(p) {
    const trailingSlash = typeof p === 'string' && p.endsWith('/') && p !== '/';
    const normalize = (path) => {
      const parts = (path || '').split('/').filter(Boolean);
      const out = [];
      for (const part of parts) {
        if (part === '.') continue;
        if (part === '..') { if (out.length > 0) out.pop(); continue; }
        out.push(part);
      }
      return out.join('/');
    };
    if (!p || p === '.') {
      return {
        serverUrl: this.serverUrl,
        volume:    this.volume,
        path:      normalize(this.cwd.replace(/^\//, '') || ''),
        trailingSlash: false,
      };
    }

    // //volume/... or //hostname:volume/...
    if (p.startsWith('//')) {
      const rest = p.slice(2);

      const firstSlash = rest.indexOf('/');
      const head = firstSlash === -1 ? rest : rest.slice(0, firstSlash);
      const tail = firstSlash === -1 ? ''   : rest.slice(firstSlash + 1);

      let serverUrl = this.serverUrl;
      let volume;

      if (head.includes(':')) {
        const [server, vol] = head.split(':');
        if (!server || !vol) throw new Error(`invalid path: ${p}`);
        serverUrl = server;
        volume    = vol;
      } else {
        volume = head;
      }

      return {
        serverUrl,
        volume,
        path: normalize(tail.replace(/\/+$/, '')),
        trailingSlash,
      };
    }

    // Absolute path in current volume.
    if (p.startsWith('/')) {
      return { serverUrl: this.serverUrl, volume: this.volume, path: normalize(p.replace(/^\/+|\/+$/g, '')), trailingSlash };
    }

    // Relative path — join with cwd.
    const base = this.cwd.replace(/^\/+|\/+$/g, '');
    const joined = base ? `${base}/${p}` : p;
    return { serverUrl: this.serverUrl, volume: this.volume, path: normalize(joined.replace(/\/+$/, '')), trailingSlash };
  }

  // ── Tool import ───────────────────────────────────────────────

  async _importTool(name) {
    if (this._importCache.has(name)) return this._importCache.get(name);

    // Validate tool name to avoid path traversal
    if (!/^[a-zA-Z0-9_-]+$/.test(name)) {
      throw new Error(`command not found: ${name}`);
    }

    const url = `../${name}/main.js`;
    let mod;
    try {
      mod = await import(url);
    } catch (e) {
      throw new Error(`command not found: ${name}`);
    }
    if (typeof mod.invoke !== 'function')
      throw new Error(`${url} has no exported invoke()`);

    this._importCache.set(name, mod);
    return mod;
  }

  // ── Help handling ─────────────────────────────────────────────

  _isHelpCall(args) {
    return args.length === 1 && _isPlainObject(args[0]) && args[0].help;
  }

  // ── Dispatch ──────────────────────────────────────────────────

  async _dispatch(name, prevResult, args) {
    const mod = await this._importTool(name);
    if (this._isHelpCall(args))
      return mod.help ?? `${name}: no help text available`;
    return mod.invoke(this, prevResult, args);
  }

  // Run a command without shell evaluation, similar to exec()
  async runCommand(name, args = [], { doHistory = true } = {}) {
    // Fork an execution shell so mutations don't affect the parent by default
    const execShell = this.fork();

    const historyIndex = doHistory ? this.__.length : null;
    if (doHistory) this.__.push(undefined);

    const root = execShell._rootResult(VOID, historyIndex, this);

    return await _dispatchWrapped(execShell, name, root, args, historyIndex);
  }

  // ── Root result (void input, start of every eval) ─────────────

  _rootResult(initial, historyIndex, parentShell) {
    return _wrapResult(new Result(this, parentShell, Promise.resolve(initial)), historyIndex);
  }

  // ── eval ──────────────────────────────────────────────────────

  async eval(src, extraVars = {}, { doHistory = false } = {}) {
    this.history.push(src);

    const __ = this.__;
    const underscore  = __.length ? __[__.length - 1] : VOID;
    const historyIndex = doHistory ? __.length : null;
    if (doHistory) __.push(undefined);

    const shell = this;
    const withTarget = new Proxy(Object.create(null), {
      has(_, key) {
        if (typeof key === 'symbol') return false;
        if (key === '__')            return true;
        if (key === '_')             return true;
        if (key === 'glob')          return true;
        if (key in globalThis)       return false;
        if (key in extraVars)        return true;
        if (key in shell._scriptScope) return true;
        // Allow any identifier; resolution happens at call time via import
        return true;
      },
      get(_, key) {
        if (typeof key === 'symbol') return undefined;
        if (key === '__') return __;
        if (key === '_')  return underscore;
        if (key === 'glob') return (pattern) => glob(shell, pattern);
        if (key in extraVars)          return extraVars[key];
        if (key in shell._scriptScope) return shell._scriptScope[key];

        // We are running a real command, apparently. We need to fork a new shell context for it.
        const execShell = shell.fork();
        const rootResult  = execShell._rootResult(VOID, historyIndex, shell);
        return (...args) => _dispatchWrapped(execShell, key, rootResult, args, historyIndex);
      },
      set(_, key, value) {
        shell._scriptScope[key] = value;
        return true;
      },
    });

    // Oh my God I hate this so so much
    const fnMatch = src.match(/^\s*function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/);
    if (fnMatch) src = `${fnMatch[1]} = ${src}`;
    let fn;
    try { fn = new Function('__w__', `with (__w__) { return (async () => (${src}))(); }`); }
    catch { fn = new Function('__w__', `with (__w__) { return (async () => { ${src} })(); }`); }

    let finalResult = await fn(withTarget);

    if (doHistory && __[historyIndex] === undefined) {
      __[historyIndex] = finalResult;
    }

    return finalResult;
  }
}

function _libKey(lib) { return `${lib.serverUrl}|${lib.volume}|${lib.path}`; }

export function makeShell(opts) { return new GimbalShell(opts); }
