import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';

export const help = `\
deny — remove ACL grants matching a pattern

Usage:
  path.deny({u:'bob'})               remove all grants for user bob
  path.deny({o:'gimbal'})            remove all grants from origin gimbal
  path.deny({u:'bob', o:'gimbal'})   remove bob's grants from origin gimbal
  path.deny({})                      remove all grants
  path.deny()                        remove all grants

Filters (all optional):
  u (or user) — only remove grants matching this user
  o (or origin) — only remove grants matching this origin

Note: unlike allow, deny does not accept 'all', 'auth', or 'permission'.`;

const FILTER_KEYS = new Set(['u', 'user', 'o', 'origin']);

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

function grantMatchesFilter(grant, filter) {
  if (filter.user !== undefined && grant.user !== filter.user) return false;
  if (filter.origin !== undefined && grant.origin !== filter.origin) return false;
  return true;
}

function getFilter(spec) {
  const filter = {};
  if (spec.u !== undefined) filter.user = spec.u;
  if (spec.user !== undefined) filter.user = spec.user;
  if (spec.o !== undefined) filter.origin = spec.o;
  if (spec.origin !== undefined) filter.origin = spec.origin;
  return filter;
}

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath))
    throw new Error('deny: must be called on a path (use gimbal.p("/path").deny({...}))');
  if (args.length > 1)
    throw new Error('deny: too many arguments');

  let filter = {};
  let hasFilter = false;

  if (args.length === 1) {
    const arg = args[0];
    if (!isPlainObject(arg))
      throw new Error('deny: argument must be a plain object');

    const keys = Object.keys(arg);
    for (const k of keys) {
      if (!FILTER_KEYS.has(k))
        throw new Error(`deny: unknown key "${k}"`);
    }

    if (arg.u !== undefined && typeof arg.u !== 'string')
      throw new Error('deny: u must be a string');
    if (arg.user !== undefined && typeof arg.user !== 'string')
      throw new Error('deny: user must be a string');
    if (arg.o !== undefined && typeof arg.o !== 'string')
      throw new Error('deny: o must be a string');
    if (arg.origin !== undefined && typeof arg.origin !== 'string')
      throw new Error('deny: origin must be a string');

    filter = getFilter(arg);
    hasFilter = keys.length > 0;
  }

  return new GimbalResult(async () => {
    const r = gimbal.resolvePath(prev.abs());
    const vol = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';

    if (r.path.includes('/.grits') || r.path.startsWith('.grits'))
      throw new Error('deny: cannot modify grants within a .grits directory');

    let cfg;
    try {
      const file = await vol.lookup(jsonPath);
      cfg = JSON.parse(JSON.stringify(await file.json()));
    } catch (e) {
      if (e.message.includes('not found')) cfg = { allow: [] };
      else throw e;
    }

    if (!hasFilter) {
      cfg.allow = [];
    } else {
      const toRemove = new Set();
      for (let i = 0; i < cfg.allow.length; i++) {
        if (grantMatchesFilter(cfg.allow[i], filter))
          toRemove.add(i);
      }
      if (toRemove.size === 0)
        throw new Error('deny: grant not found');
      cfg.allow = cfg.allow.filter((_, i) => !toRemove.has(i));
    }

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
