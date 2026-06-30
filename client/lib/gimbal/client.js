import { createDispatchProxy } from './dispatch.js';
import { GimbalPath } from './path.js';

class Volume {
  constructor(gimbal, name) {
    this._gimbal = gimbal;
    this._name = name;
  }

  get name() { return this._name; }
  get _gritsVolume() { return this._gimbal.grits.volume(this._gimbal._serverUrl, this._name); }

  path(p) {
    const abs = p.startsWith('/') ? p : '/' + p;
    return new GimbalPath(abs, this._gimbal);
  }
  p(p) { return this.path(p); }
}

export class GimbalClient {
  constructor(gritsClient) {
    this.grits = gritsClient;
    this._importCache = new Map();
    this._volumes = new Map();
    this._serverUrl = gritsClient._serverUrl || (
      typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080'
    );
    return createDispatchProxy(this, this);
  }

  volume(name = 'primary') {
    let v = this._volumes.get(name);
    if (!v) {
      v = new Volume(this, name);
      this._volumes.set(name, v);
    }
    return v;
  }

  path(pathStr) {
    return this.volume().path(pathStr);
  }
  p(pathStr) { return this.path(pathStr); }

  resolvePath(str) {
    if (str.startsWith('//')) {
      const slash = str.indexOf('/', 2);
      const volumeName = str.substring(2, slash);
      const path = str.substring(slash);
      return { volumeName, path };
    }
    if (!str.startsWith('/')) {
      str = '/' + str;
    }
    return { volumeName: 'primary', path: str };
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

  site(hostname) {
    hostname = hostname || (this._serverUrl ? new URL(this._serverUrl).hostname : 'localhost');
    return this.p(`/sites/${hostname}/live`);
  }

  root() {
    return this.p('/');
  }

  eval(src, extraVars = {}) {
    const fnArgs = ['gimbal', ...Object.keys(extraVars)];
    const fnValues = [window.gimbal, ...Object.values(extraVars)];

    const fnMatch = src.match(/^\s*function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(/);
    if (fnMatch) src = `${fnMatch[1]} = ${src}`;

    let fn;
    try {
      fn = new Function(...fnArgs, `return (async () => (${src}))();`);
    } catch {
      fn = new Function(...fnArgs, `return (async () => { ${src} })();`);
    }

    return fn(...fnValues);
  }
}

let _instance = null;

export function getGimbal() {
  if (!_instance) throw new Error('GimbalClient not initialized');
  return _instance;
}

export function initGimbal(gritsClient) {
  if (!_instance) {
    _instance = new GimbalClient(gritsClient);
  }
  return _instance;
}
