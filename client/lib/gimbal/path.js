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

  path(relative) {
    const baseParts = this._path.split('/').filter(Boolean);
    const relParts = String(relative).split('/').filter(Boolean);
    for (const part of relParts) {
      if (part === '.') continue;
      if (part === '..') { if (baseParts.length > 0) baseParts.pop(); continue; }
      baseParts.push(part);
    }
    return new GimbalPath('/' + baseParts.join('/'), window.gimbal);
  }
  p(relative) { return this.path(relative); }

  relPath(relative) {
    const parts = this._path.split('/').filter(Boolean);
    if (parts.length > 0) parts.pop();
    const parent = '/' + parts.join('/');
    const baseParts = parent.split('/').filter(Boolean);
    const relParts = String(relative).split('/').filter(Boolean);
    for (const part of relParts) {
      if (part === '.') continue;
      if (part === '..') { if (baseParts.length > 0) baseParts.pop(); continue; }
      baseParts.push(part);
    }
    return new GimbalPath('/' + baseParts.join('/'), window.gimbal);
  }
}
