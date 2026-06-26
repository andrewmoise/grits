import { GimbalResult } from './result.js';
import { GimbalPath } from './path.js';
import { createDispatchProxy, wrapResult, SHORTCUTS } from './dispatch.js';

export { GimbalResult, GimbalPath, createDispatchProxy, wrapResult, SHORTCUTS };

export const VOID = Object.freeze({ _gimbalVoid: true, toString: () => '(void)' });

export function _isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

const HISTORY_NEW = Symbol('HISTORY_NEW');

export class GimbalShell {
  constructor({ fs, serverUrl, volume, cwd, gwm }) {
    this.fs = fs;
    this.serverUrl = serverUrl;
    this.volume = volume;
    this.cwd = cwd || '/';
    this.gwm = gwm ?? null;
    this._scriptScope = Object.create(null);
    this.history = [];
    this.__ = [];
    this._importCache = new Map();

    return createDispatchProxy(this, this);
  }

  fork() {
    const child = new GimbalShell({
      fs: this.fs,
      serverUrl: this.serverUrl,
      volume: this.volume,
      cwd: this.cwd,
      gwm: this.gwm,
    });
    child._importCache = this._importCache;
    return child;
  }

  get ui() { return null; }

  _currentVol() { return this._vol(null, null); }

  _vol(serverUrl, vol) {
    return this.fs.volume(
      serverUrl ?? this.serverUrl,
      vol ?? this.volume
    );
  }

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
        volume: this.volume,
        path: normalize(this.cwd.replace(/^\//, '') || ''),
        trailingSlash: false,
      };
    }

    if (p.startsWith('//')) {
      const rest = p.slice(2);
      const firstSlash = rest.indexOf('/');
      const head = firstSlash === -1 ? rest : rest.slice(0, firstSlash);
      const tail = firstSlash === -1 ? '' : rest.slice(firstSlash + 1);

      let serverUrl = this.serverUrl;
      let volume;

      if (head.includes(':')) {
        const [server, vol] = head.split(':');
        if (!server || !vol) throw new Error(`invalid path: ${p}`);
        serverUrl = server;
        volume = vol;
      } else {
        volume = head;
      }

      return { serverUrl, volume, path: normalize(tail.replace(/\/+$/, '')), trailingSlash };
    }

    if (p.startsWith('/')) {
      return { serverUrl: this.serverUrl, volume: this.volume, path: normalize(p.replace(/^\/+|\/+$/g, '')), trailingSlash };
    }

    const base = this.cwd.replace(/^\/+|\/+$/g, '');
    const joined = base ? `${base}/${p}` : p;
    return { serverUrl: this.serverUrl, volume: this.volume, path: normalize(joined.replace(/\/+$/, '')), trailingSlash };
  }

  async _importTool(name) {
    if (this._importCache.has(name)) return this._importCache.get(name);

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

  _isHelpCall(args) {
    return args.length === 1 && _isPlainObject(args[0]) && args[0].help;
  }

  async runCommand(name, args = [], { doHistory = true } = {}) {
    const mod = await this._importTool(name);
    const result = mod.invoke(this, ...args);

    if (doHistory) {
      const val = result instanceof GimbalResult ? await result : result;
      this.__.push(val);
    }

    return result;
  }

  async importLib(gritsPath) {
    const r = this.resolvePath(gritsPath);
    const url = this.serverUrl + '/grits/v1/content/' + r.volume + '/' + r.path;
    return import(url);
  }

  async eval(src, extraVars = {}, { doHistory = false } = {}) {
    this.history.push(src);

    const __ = this.__;
    const underscore = __.length ? __[__.length - 1] : null;
    const historyIndex = doHistory ? __.length : null;
    if (doHistory) __.push(undefined);

    const shell = this;

    const fnArgs = ['gsh', 'gwm', '_', '__', ...Object.keys(extraVars)];
    const fnValues = [shell, shell.gwm, underscore, __, ...Object.values(extraVars)];

    const fnMatch = src.match(/^\s*function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/);
    if (fnMatch) src = `${fnMatch[1]} = ${src}`;

    let fn;
    try {
      fn = new Function(...fnArgs, `return (async () => (${src}))();`);
    } catch {
      fn = new Function(...fnArgs, `return (async () => { ${src} })();`);
    }

    let finalResult = await fn(...fnValues);

    if (doHistory && historyIndex !== null && __[historyIndex] === undefined) {
      __[historyIndex] = finalResult;
    }

    return finalResult;
  }
}

export function makeShell(opts) {
  const shell = new GimbalShell(opts);
  return shell;
}
