import { createDispatchProxy } from './dispatch.js';

export class GimbalPath {
  constructor(absPath, shell) {
    this._absPath = absPath;
    this._shell = shell;
    return createDispatchProxy(this, shell);
  }

  toJSON() {
    return this._absPath;
  }

  abs() {
    return this._absPath;
  }

  toString() {
    const parts = this._absPath.split('/').filter(Boolean);
    return parts.length ? parts[parts.length - 1] : '/';
  }
}
