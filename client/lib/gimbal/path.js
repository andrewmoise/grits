import { createDispatchProxy } from './dispatch.js';

export class GimbalPath {
  constructor(path, gimbal) {
    this._path = path;
    return createDispatchProxy(this, gimbal);
  }

  toJSON() {
    return this._path;
  }

  abs() {
    return this._path;
  }

  toString() {
    const parts = this._path.split('/').filter(Boolean);
    return parts.length ? parts[parts.length - 1] : '/';
  }

  p(relative) {
    const baseParts = this._path.split('/').filter(Boolean);
    const relParts = String(relative).split('/').filter(Boolean);
    for (const part of relParts) {
      if (part === '.') continue;
      if (part === '..') { if (baseParts.length > 0) baseParts.pop(); continue; }
      baseParts.push(part);
    }
    return new GimbalPath('/' + baseParts.join('/'), window.gimbal);
  }
}
