// lib/facl/main.js
export const help = `\
facl — display or modify file ACL grants

Usage:
  facl()                                      list grants on current dir
  facl('/sites')                              list grants on /sites
  facl({u:'moise', o:'gimbal'}, {p:'owner'})  add/modify on current dir
  facl({u:'moise'}, {x:1})                    remove on current dir
  facl('/sites', {u:'moise', o:'gimbal'}, {p:'owner'})
  facl('/sites', {u:'moise'}, {x:1})

Auth/wildcard principals:
  facl({auth:true}, {p:'read'})                 grant all authenticated users
  facl({all:true}, {p:'read'})                  grant everyone (including anonymous)
  facl({auth:true}, {x:1})                      remove grant for authenticated users
  facl({all:true}, {x:1})                       remove grant for everyone

Multiple paths and principals can be combined:
  facl('/a', '/b', {u:'alice'}, {x:1})                     remove on both paths
  facl({u:'alice', o:'*'}, {u:'bob', o:'*'}, {p:'read'})   add two grants on one path
  facl({x:1})                                              clear all grants on current dir
  facl({p:'read'}, {x:1})                                  remove all read grants

Principal fields:  u / user, o / origin (required)
                   auth
                   all
Grant fields:      p / permission, x

Permissions: read, insert, read+insert, read+write, owner

Note: origin must be specified when adding grants.
  Use {o:"*"} for any origin, a single word like {o:"gimbal"} to
  expand to your server's subdomain, or an explicit URL like
  {o:"https://app.example.com"} for a specific origin.`;

import { isVoid, VOID, _isPlainObject, responseFromJSON } from '../gimbal/gsh.js';

const PRINCIPAL_KEYS = new Set(['u', 'user', 'auth', 'all', 'o', 'origin']);
const ACTION_KEYS    = new Set(['p', 'permission', 'x']);

// Normalize a principal descriptor — expands u→user, nothing else needed.
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

// Normalize a permission spec — expands p→permission.
function normalizePermission(spec) {
  const g = {};
  if (spec.p !== undefined) g.permission = spec.p;
  if (spec.permission !== undefined) g.permission = spec.permission;
  return g;
}

// Match a grant against a principal descriptor (identity only, ignores permission).
function grantMatchesPrincipal(grant, principal) {
  if (principal.user !== undefined && grant.user !== principal.user) return false;
  if (principal.auth !== undefined && grant.auth !== principal.auth) return false;
  if (principal.all !== undefined && grant.all !== principal.all) return false;
	if (principal.origin !== undefined && principal.origin !== '*' && grant.origin !== principal.origin) return false;
  return true;
}

// Match a grant against a strict permission spec (permission must match if provided).
function grantMatchesPermission(grant, permSpec) {
  if (permSpec.permission !== undefined && grant.permission !== permSpec.permission) return false;
  return true;
}

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('facl: does not accept pipeline input');

  // Classify each argument.
  const paths      = [];
  const principals = [];
  let   permission = null;
  let   remove     = false;

  for (const arg of args) {
    if (typeof arg === 'string') {
      paths.push(arg);
      continue;
    }

    if (!_isPlainObject(arg))
      throw new Error('facl: arguments must be strings or objects');

    const keys = Object.keys(arg);

    if (keys.length === 0)
      throw new Error('facl: empty map argument');

    const isPrincipal = keys.every(k => PRINCIPAL_KEYS.has(k));
    const isAction    = keys.every(k => ACTION_KEYS.has(k));

    if (isPrincipal && !isAction) {
      principals.push(normalizePrincipal(arg));
    } else if (isAction && !isPrincipal) {
      if (arg.x) {
        remove = true;
      }
      if (arg.p !== undefined || arg.permission !== undefined) {
        const p = normalizePermission(arg);
        if (permission !== null && permission.permission !== p.permission) {
          throw new Error('facl: conflicting permission values');
        }
        permission = p;
      }
      if (!arg.x && arg.p === undefined && arg.permission === undefined) {
        throw new Error(`facl: unknown action key in ${JSON.stringify(arg)}`);
      }
    } else {
      throw new Error(
        'facl: argument keys must be all principal fields or all action fields, not mixed'
      );
    }
  }

  // Default path.
  if (paths.length === 0) paths.push('.');

  // List mode — no principals and no action.
  if (principals.length === 0 && !remove && permission === null) {
    if (paths.length > 1) return VOID; // multi-path listing not supported
    const r   = shell.resolvePath(paths[0]);
    const vol = shell._vol(r.serverUrl, r.volume);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';
    let cfg;
    try {
      const file = await vol.lookup(jsonPath);
      cfg = await file.json();
    } catch { cfg = { allow: [] }; }
    return responseFromJSON(cfg);
  }

  // Validate.
  if (!remove && permission === null) {
    throw new Error('facl: specify a permission with {p:"..."} to add grants');
  }
  if (!remove && principals.length === 0) {
    throw new Error('facl: specify at least one principal (u:, auth, all) to add grants');
  }
	if (!remove && principals.every(p => p.origin === undefined)) {
		throw new Error('facl: origin is required. Use {o:"*"} for any origin, a single word like {o:"gimbal"} to expand to the matching subdomain, or an explicit URL.');
	}

  // Process each path.
  for (const targetPath of paths) {
    const r    = shell.resolvePath(targetPath);
    const vol  = shell._vol(r.serverUrl, r.volume);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';

    if (r.path.includes('/.grits') || r.path.startsWith('.grits')) {
      throw new Error('facl: cannot modify grants within a .grits directory');
    }

    let cfg;
    try {
      const file = await vol.lookup(jsonPath);
      cfg = JSON.parse(JSON.stringify(await file.json()));
    } catch { cfg = { allow: [] }; }

    if (remove) {
      // ── Remove mode ──────────────────────────────────────────
      if (principals.length === 0 && permission === null) {
        // {x:1} alone — clear all grants.
        cfg.allow = [];
      } else {
        const toRemove = new Set();
        for (let i = 0; i < cfg.allow.length; i++) {
          const grant = cfg.allow[i];
          let matches = false;
          // Match against any principal.
          for (const p of principals) {
            if (grantMatchesPrincipal(grant, p)) { matches = true; break; }
          }
          // If no principals specified, match against permission only.
          if (principals.length === 0) matches = true;
          if (matches && permission !== null) {
            matches = grantMatchesPermission(grant, permission);
          }
          if (matches) toRemove.add(i);
        }
        if (toRemove.size === 0) {
          throw new Error('facl: grant not found');
        }
        cfg.allow = cfg.allow.filter((_, i) => !toRemove.has(i));
      }
    } else {
      // ── Add/update mode ──────────────────────────────────────
      for (const principal of principals) {
        const idx = cfg.allow.findIndex(g => grantMatchesPrincipal(g, principal));
        const newGrant = { ...principal, ...permission };
        if (idx !== -1) {
          cfg.allow[idx] = newGrant;
        } else {
          cfg.allow.push(newGrant);
        }
      }
    }

    // Ensure .grits directory exists before writing.
    const gritsDir = r.path ? r.path + '/.grits' : '.grits';
    try { await vol.lookup(gritsDir); } catch {
      const dirCID = await vol.mkdir({});
      await vol.link(dirCID, gritsDir);
    }

    const raw   = JSON.stringify(cfg, null, 2);
    const bytes = new TextEncoder().encode(raw);
    const cid   = await vol.put(bytes);
    const meta  = await vol.mkfile(cid, bytes.length);
    await vol.link(meta, jsonPath);
  }

  return VOID;
}
