import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
allow — add or modify an ACL grant

Usage:
  path.allow({u:'alice', o:'gimbal', p:'owner'})   add/modify grant

Required fields:
  u (or user) — username, or all:true / auth:true
  o (or origin) — origin (e.g. "gimbal", "https://...", "*")
  p (or permission) — permission level (e.g. "read", "owner", "read+write")`;

const SPEC_KEYS = new Set(['u', 'user', 'all', 'auth', 'o', 'origin', 'p', 'permission']);

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

function normalizeSpec(spec) {
  const out = {};
  if (spec.u !== undefined) out.user = spec.u;
  if (spec.user !== undefined) out.user = spec.user;
  if (spec.all !== undefined) out.all = spec.all;
  if (spec.auth !== undefined) out.auth = spec.auth;
  if (spec.o !== undefined) out.origin = spec.o;
  if (spec.origin !== undefined) out.origin = spec.origin;
  if (spec.p !== undefined) out.permission = spec.p;
  if (spec.permission !== undefined) out.permission = spec.permission;
  return out;
}

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath))
    throw new Error('allow: must be called on a path (use gimbal.p("/path").allow({...}))');
  if (args.length !== 1)
    throw new Error('allow: expected exactly one argument (the spec object)');

  const arg = args[0];
  if (!isPlainObject(arg))
    throw new Error('allow: argument must be a plain object');

  const keys = Object.keys(arg);
  if (keys.length === 0)
    throw new Error('allow: empty spec object');

  for (const k of keys) {
    if (!SPEC_KEYS.has(k))
      throw new Error(`allow: unknown key "${k}"`);
  }

  if (arg.all !== undefined && typeof arg.all !== 'boolean')
    throw new Error('allow: all must be a boolean');
  if (arg.auth !== undefined && typeof arg.auth !== 'boolean')
    throw new Error('allow: auth must be a boolean');
  if (arg.u !== undefined && typeof arg.u !== 'string')
    throw new Error('allow: u must be a string');
  if (arg.user !== undefined && typeof arg.user !== 'string')
    throw new Error('allow: user must be a string');
  if (arg.o !== undefined && typeof arg.o !== 'string')
    throw new Error('allow: o must be a string');
  if (arg.origin !== undefined && typeof arg.origin !== 'string')
    throw new Error('allow: origin must be a string');
  if (arg.p !== undefined && typeof arg.p !== 'string')
    throw new Error('allow: p must be a string');
  if (arg.permission !== undefined && typeof arg.permission !== 'string')
    throw new Error('allow: permission must be a string');

  const principalFields = ['u', 'user', 'all', 'auth'];
  const hasPrincipal = principalFields.some(k => arg[k] !== undefined);
  if (!hasPrincipal)
    throw new Error('allow: specify a principal: u, user, all, or auth');

  const hasOrigin = arg.o !== undefined || arg.origin !== undefined;
  if (!hasOrigin)
    throw new Error('allow: origin (o) is required. Use {o:"*"} for any origin.');

  const hasPerm = arg.p !== undefined || arg.permission !== undefined;
  if (!hasPerm)
    throw new Error('allow: permission (p) is required');

  const spec = normalizeSpec(arg);

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(prev.abs());
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';

    if (r.path.includes('/.grits') || r.path.startsWith('.grits'))
      throw new Error('allow: cannot modify grants within a .grits directory');

    let cfg;
    try {
      const file = await vol.lookup(jsonPath);
      cfg = JSON.parse(JSON.stringify(await file.json()));
    } catch (e) {
      if (e.message.includes('not found')) cfg = { allow: [] };
      else throw e;
    }

    const idx = cfg.allow.findIndex(g => {
      if (spec.user !== undefined && g.user !== spec.user) return false;
      if (spec.all !== undefined && g.all !== spec.all) return false;
      if (spec.auth !== undefined && g.auth !== spec.auth) return false;
      if (spec.origin !== undefined && g.origin !== spec.origin) return false;
      return true;
    });

    if (idx !== -1) cfg.allow[idx] = spec;
    else cfg.allow.push(spec);

    const gritsDir = r.path ? r.path + '/.grits' : '.grits';
    try { await vol.lookup(gritsDir); } catch {
      const dirCID = await vol.mkdir({});
      await vol.link(dirCID, gritsDir);
    }

    const raw = JSON.stringify(cfg, null, 2);
    const bytes = new TextEncoder().encode(raw);
    const cid = await vol.put(bytes);
    const meta = await vol.mkfile(cid, bytes.length);
    await vol.link(meta, jsonPath);
  });
}
