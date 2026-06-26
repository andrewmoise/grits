import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
facl — display or modify file ACL grants

Usage:
  gsh.facl({u:'moise', o:'gimbal'}, {p:'owner'})  add/modify on current dir
  gsh.facl(path, {u:'moise'}, {x:1})               remove on path
  gsh.facl(path)                                   list grants on path

Auth/wildcard:
  {auth:true}, {all:true}, {o:"*"} for any origin

Multiple paths and principals can be combined.`;

const PRINCIPAL_KEYS = new Set(['u', 'user', 'auth', 'all', 'o', 'origin']);
const ACTION_KEYS = new Set(['p', 'permission', 'x']);

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

function normalizePrincipal(spec) {
  const p = {};
  if (spec.u !== undefined) p.user = spec.u;
  if (spec.user !== undefined) p.user = spec.user;
  if (spec.auth !== undefined) p.auth = spec.auth;
  if (spec.all !== undefined) p.all = spec.all;
  if (spec.o !== undefined) p.origin = spec.o;
  if (spec.origin !== undefined) p.origin = spec.origin;
  return p;
}

function normalizePermission(spec) {
  const g = {};
  if (spec.p !== undefined) g.permission = spec.p;
  if (spec.permission !== undefined) g.permission = spec.permission;
  return g;
}

function grantMatchesPrincipal(grant, principal) {
  if (principal.user !== undefined && grant.user !== principal.user) return false;
  if (principal.auth !== undefined && grant.auth !== principal.auth) return false;
  if (principal.all !== undefined && grant.all !== principal.all) return false;
  if (principal.origin !== undefined && principal.origin !== '*' && grant.origin !== principal.origin) return false;
  return true;
}

function grantMatchesPermission(grant, permSpec) {
  if (permSpec.permission !== undefined && grant.permission !== permSpec.permission) return false;
  return true;
}

export function invoke(prev, ...args) {
  if (!(prev instanceof GimbalShell)) throw new Error('facl: must be called on gsh');
  const shell = prev;

  const paths = [];
  const principals = [];
  let permission = null;
  let remove = false;

  for (const arg of args) {
    if (arg instanceof GimbalPath) {
      paths.push(arg.abs());
      continue;
    }
    if (typeof arg === 'string') { paths.push(arg); continue; }
    if (!isPlainObject(arg)) throw new Error('facl: arguments must be strings, GimbalPaths, or objects');

    const keys = Object.keys(arg);
    if (keys.length === 0) throw new Error('facl: empty map argument');

    const isPrincipal = keys.every(k => PRINCIPAL_KEYS.has(k));
    const isAction = keys.every(k => ACTION_KEYS.has(k));

    if (isPrincipal && !isAction) {
      principals.push(normalizePrincipal(arg));
    } else if (isAction && !isPrincipal) {
      if (arg.x) remove = true;
      if (arg.p !== undefined || arg.permission !== undefined) {
        const p = normalizePermission(arg);
        if (permission !== null && permission.permission !== p.permission)
          throw new Error('facl: conflicting permission values');
        permission = p;
      }
      if (!arg.x && arg.p === undefined && arg.permission === undefined)
        throw new Error(`facl: unknown action key in ${JSON.stringify(arg)}`);
    } else {
      throw new Error('facl: argument keys must be all principal fields or all action fields');
    }
  }

  if (paths.length === 0) paths.push('.');

  return new GimbalResult(async () => {
    if (principals.length === 0 && !remove && permission === null) {
      if (paths.length > 1) return;
      const r = shell.resolvePath(paths[0]);
      const vol = shell._vol(r.serverUrl, r.volume);
      const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';
      try {
        const file = await vol.lookup(jsonPath);
        return await file.json();
      } catch (e) {
        if (e.message.includes('not found')) return;
        throw e;
      }
    }

    if (!remove && permission === null) throw new Error('facl: specify a permission with {p:"..."}');
    if (!remove && principals.length === 0) throw new Error('facl: specify at least one principal');
    if (!remove && principals.every(p => p.origin === undefined))
      throw new Error('facl: origin is required. Use {o:"*"} for any origin.');

    for (const targetPath of paths) {
      const r = shell.resolvePath(targetPath);
      const vol = shell._vol(r.serverUrl, r.volume);
      const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';

      if (r.path.includes('/.grits') || r.path.startsWith('.grits'))
        throw new Error('facl: cannot modify grants within a .grits directory');

      let cfg;
      try {
        const file = await vol.lookup(jsonPath);
        cfg = JSON.parse(JSON.stringify(await file.json()));
      } catch (e) {
        if (e.message.includes('not found')) cfg = { allow: [] };
        else throw e;
      }

      if (remove) {
        if (principals.length === 0 && permission === null) {
          cfg.allow = [];
        } else {
          const toRemove = new Set();
          for (let i = 0; i < cfg.allow.length; i++) {
            const grant = cfg.allow[i];
            let matches = principals.length === 0;
            for (const p of principals) { if (grantMatchesPrincipal(grant, p)) { matches = true; break; } }
            if (matches && permission !== null) matches = grantMatchesPermission(grant, permission);
            if (matches) toRemove.add(i);
          }
          if (toRemove.size === 0) throw new Error('facl: grant not found');
          cfg.allow = cfg.allow.filter((_, i) => !toRemove.has(i));
        }
      } else {
        for (const principal of principals) {
          const idx = cfg.allow.findIndex(g => grantMatchesPrincipal(g, principal));
          const newGrant = { ...principal, ...permission };
          if (idx !== -1) cfg.allow[idx] = newGrant;
          else cfg.allow.push(newGrant);
        }
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
    }
  });
}
