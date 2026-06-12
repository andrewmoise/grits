// lib/facl/main.js
export const help = `\
facl — display or modify file ACL grants

Usage:
  facl()                                list grants on current directory
  facl('/sites')                        list grants on /sites
  facl({u:'moise', p:'owner'})          add/modify grant on current dir
  facl({u:'moise', p:'owner'}, {x:1})   remove grant on current dir
  facl('/sites', {u:'moise', p:'owner'})
  facl('/sites', {u:'moise', p:'owner'}, {x:1})

Multiple paths and grants can be combined:
  facl('/a', '/b', {u:'alice'}, {x:1})        remove on both paths
  facl({u:'alice'}, {u:'bob'}, '/sites')      add two grants on one path

Grant fields:
  u:     username (alias for "user")
  p:     permission (alias for "permission")
  auth   true for any authenticated user
  all    true for any principal including anonymous

Permissions: read, insert, read+insert, read+write, owner`;

import { isVoid, VOID, _isPlainObject, responseFromJSON } from '../gimbal/gsh.js';

const ACL_KEYS    = new Set(['u', 'user', 'auth', 'all', 'p', 'permission', 'origin']);
const OPTION_KEYS = new Set(['x']);

// Normalize convenience aliases in a grant object.
function normalizeGrant(spec) {
  const g = {};
  if (spec.u !== undefined) g.user = spec.u;
  if (spec.user !== undefined) g.user = spec.user;
  if (spec.p !== undefined) g.permission = spec.p;
  if (spec.permission !== undefined) g.permission = spec.permission;
  if (spec.auth !== undefined) g.auth = spec.auth;
  if (spec.all !== undefined) g.all = spec.all;
  if (spec.origin !== undefined) g.origin = spec.origin;
  return g;
}

// Match a grant against an identity spec — checks only identity fields
// (user, auth, all). Permission is ignored. Used for add/update.
function grantMatchesIdentity(grant, spec) {
  if (spec.user !== undefined && grant.user !== spec.user) return false;
  if (spec.auth !== undefined && grant.auth !== spec.auth) return false;
  if (spec.all !== undefined && grant.all !== spec.all) return false;
  return true;
}

// Match a grant against a strict spec — all provided fields including
// permission must match. Used for remove.
function grantMatchesStrict(grant, spec) {
  if (spec.user !== undefined && grant.user !== spec.user) return false;
  if (spec.auth !== undefined && grant.auth !== spec.auth) return false;
  if (spec.all !== undefined && grant.all !== spec.all) return false;
  if (spec.permission !== undefined && grant.permission !== spec.permission) return false;
  return true;
}

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('facl: does not accept pipeline input');

  // Classify each argument: path (string), grant spec, or options.
  const paths  = [];
  const grants = [];
  let   opts   = {};

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

    const isACL    = keys.every(k => ACL_KEYS.has(k));
    const isOption = keys.every(k => OPTION_KEYS.has(k));

    if (isACL && !isOption) {
      grants.push(normalizeGrant(arg));
    } else if (isOption && !isACL) {
      opts = { ...opts, ...arg };
    } else {
      throw new Error(
        'facl: ambiguous argument — keys must be all ACL fields or all options, not mixed'
      );
    }
  }

  // Default path.
  if (paths.length === 0) paths.push('.');

  // Process each path.
  for (const targetPath of paths) {
    const r    = shell.resolvePath(targetPath);
    const vol  = shell._vol(r.serverUrl, r.volume);
    const jsonPath = r.path ? r.path + '/.grits/access.json' : '.grits/access.json';

    // Guard: refuse to create/manage .grits within .grits.
    if (r.path.includes('/.grits') || r.path.startsWith('.grits')) {
      throw new Error('facl: cannot modify grants within a .grits directory');
    }

    // Try reading existing access.json.
    let cfg;
    try {
      const file = await vol.lookup(jsonPath);
      cfg = await file.json();
    } catch {
      cfg = { allow: [] };
    }

    // List mode — no grants specified.
    if (grants.length === 0) {
      // Only list the first path if there are multiple; multi-path listing
      // doesn't make sense without grants.
      if (paths.length > 1) continue;
      return responseFromJSON(cfg);
    }

    // Apply each grant.
    for (const grantSpec of grants) {
      if (opts.x) {
        // Remove mode — strict match (all provided fields including permission).
        const idx = cfg.allow.findIndex(g => grantMatchesStrict(g, grantSpec));
        if (idx === -1) {
          throw new Error('facl: grant not found');
        }
        cfg.allow.splice(idx, 1);
      } else {
        // Add/update mode — identity match only (user/auth/all, ignores permission).
        const idx = cfg.allow.findIndex(g => grantMatchesIdentity(g, grantSpec));
        if (idx !== -1) {
          cfg.allow[idx] = grantSpec;
        } else {
          cfg.allow.push(grantSpec);
        }
      }
    }

    // Ensure .grits directory exists before writing.
    const gritsDir = r.path ? r.path + '/.grits' : '.grits';
    try {
      await vol.lookup(gritsDir);
    } catch {
      const dirCID = await vol.mkdir({});
      await vol.link(dirCID, gritsDir);
    }

    // Write back.
    const raw    = JSON.stringify(cfg, null, 2);
    const bytes  = new TextEncoder().encode(raw);
    const cid    = await vol.put(bytes);
    const meta   = await vol.mkfile(cid, bytes.length);
    await vol.link(meta, jsonPath);
  }

  return VOID;
}
