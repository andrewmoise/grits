// GritsClient.js — Gimbal filesystem client
//
// GritsClient — filesystem operations (local + browser cache, no server contact)
//   fs.cacheGet(cidString)                → Response | null
//   fs.cachePut(bytes)                    → string (content CID, stored locally)
//   fs.mkfile(cidString, size)            → string (metadata CID, stored locally)
//   fs.mkdir(entries?)                    → string (metadata CID, stored locally)
//   fs.gc(cidString?)                     → void
//
// GritsVolume — server operations (with shared cache fallthrough)
//   vol.lookup (path)                     → GritsFile
//   vol.fileForCID (cidString)            → GritsFile
//   vol.link   (file|cid, path)           → LinkResponse
//   vol.get    (cidString)                → Response
//   vol.put    (bytes)                    → string (content CID)
//   vol.meta(cidString)                   → { type, size, contentHash, ... }
//   vol.json(cidString)                   → parsed JS object
//   vol.mkfile(cidString, size)           → string (metadata CID, stored locally)
//   vol.mkdir(entries?)                   → string (metadata CID, stored locally)
//   vol.gc(cidString?)                    → void
//   vol.resetRoot()                       → void
//   vol.getServiceWorkerHash()            → string | undefined
//
// GritsFile — obtained from vol.lookup():
//   file.cid()        → string  (metadata CID — use this for li())
//   file.contentCID() → string  (content blob CID — use this for get())
//   file.size()       → number
//   file.isDir()      → boolean
//   file.isFile()     → boolean
//   file.meta()       → { type, size, contentHash, mode, timestamp }
//   file.children()   → { name: GritsFile } for a directory's children
//   file.get()        → Promise<Response>
//   file.bytes()      → Promise<ArrayBuffer>
//   file.text()       → Promise<string>
//   file.json()       → Promise<any>

import MirrorManager      from './MirrorManager.js';      // %FOR MODULE%
import HashVerifier       from './HashVerifier.js';        // %FOR MODULE%
import PerformanceTracker from './PerformanceTracker.js';  // %FOR MODULE%
//importScripts('/grits/v1/content/client/MirrorManager-sw.js');      // %FOR SERVICEWORKER%
//importScripts('/grits/v1/content/client/HashVerifier-sw.js');        // %FOR SERVICEWORKER%
//importScripts('/grits/v1/content/client/PerformanceTracker-sw.js');  // %FOR SERVICEWORKER%

const DEBUG       = false;
const DEBUG_STATS = true;
const JSON_CACHE_MAX_AGE          = 5 * 60 * 1000;
const JSON_CACHE_CLEANUP_INTERVAL = 5 * 60 * 1000;
const DEFAULT_HARD_TIMEOUT        = 1 * 60 * 1000; // 1 minute, matches Go-side default
const MINIROOT_TTL                = 1 * 60 * 1000; // 1 minute

// ─────────────────────────────────────────────────────────────────
// MultiLink assertion flags — mirror of Go-side constants in namestore.go
// ─────────────────────────────────────────────────────────────────

export const ASSERT_PREV_MATCHES = 1;
export const ASSERT_IS_BLOB      = 2;
export const ASSERT_IS_TREE      = 4;
export const ASSERT_IS_NONEMPTY  = 8;

export class AssertionError extends Error {
  constructor(msg) {
    super(msg);
    this.name = 'AssertionError';
  }
}

export class AccessDeniedError extends Error {
  constructor(path) {
    super(`access denied: ${path}`);
    this.name  = 'AccessDeniedError';
    this.path  = path;
  }
}

// ─────────────────────────────────────────────────────────────────
// Type assertion helpers
// ─────────────────────────────────────────────────────────────────

function _typename(v) {
  if (v === null)      return 'null';
  if (v === undefined) return 'undefined';
  return v?.constructor?.name ?? typeof v;
}

function _assertString(v, label) {
  if (typeof v !== 'string')
    throw new TypeError(
      `${label}: expected CID string, got ${_typename(v)}` +
      (v instanceof GritsFile ? ' — did you mean file.cid() or file.contentCID()?' : ''));
}

function _assertBytes(v, label) {
  if (!(v instanceof Uint8Array) && !(v instanceof ArrayBuffer) &&
      !(v instanceof Blob) && !(v instanceof ReadableStream))
    throw new TypeError(
      `${label}: expected bytes (Uint8Array/ArrayBuffer/Blob/ReadableStream), got ${_typename(v)}`);
}

function _assertStringOrFile(v, label) {
  if (typeof v !== 'string' && !(v instanceof GritsFile))
    throw new TypeError(`${label}: expected CID string or GritsFile, got ${_typename(v)}`);
}

function _assertEntriesMap(v, label) {
  if (v !== null && v !== undefined && (typeof v !== 'object' || Array.isArray(v)))
    throw new TypeError(
      `${label}: expected object { name: GritsFile|cidString, ... }, got ${_typename(v)}`);
}

// ─────────────────────────────────────────────────────────────────
// Duration parser — matches Go's time.Duration string format
// e.g. "30s", "5m", "2h", "24h", "1m30s"
// Returns milliseconds, or null if unparseable.
// ─────────────────────────────────────────────────────────────────

function _parseDuration(s) {
  if (typeof s !== 'string' || s.length === 0) return null;
  const units = { ns: 1e-6, us: 1e-3, µs: 1e-3, ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
  const re = /([0-9]*\.?[0-9]+)(ns|us|µs|ms|[smh])/g;
  let match, total = 0, matched = false;
  while ((match = re.exec(s)) !== null) {
    const val = parseFloat(match[1]);
    const mul = units[match[2]];
    if (mul == null || isNaN(val)) return null;
    total += val * mul;
    matched = true;
  }
  return matched ? total : null;
}

// ─────────────────────────────────────────────────────────────────
// Hashing (SHA-256 → Base58 multihash, matching server format)
// ─────────────────────────────────────────────────────────────────

const BASE58_ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';

function _base58Encode(buffer) {
  let zeros = 0;
  while (zeros < buffer.length && buffer[zeros] === 0) zeros++;
  const size = Math.floor((buffer.length - zeros) * 138 / 100) + 1;
  const b58  = new Uint8Array(size);
  let length = 0;
  for (let i = zeros; i < buffer.length; i++) {
    let carry = buffer[i], j = 0;
    for (let k = b58.length - 1; k >= 0; k--, j++) {
      if (carry === 0 && j >= length) break;
      carry += 256 * b58[k];
      b58[k] = carry % 58;
      carry  = Math.floor(carry / 58);
    }
    length = j;
  }
  let i = b58.length - length;
  while (i < b58.length && b58[i] === 0) i++;
  let str = '1'.repeat(zeros);
  for (; i < b58.length; i++) str += BASE58_ALPHABET[b58[i]];
  return str;
}

async function _computeCID(data) {
  const digest    = new Uint8Array(await crypto.subtle.digest('SHA-256', data));
  const multihash = new Uint8Array(34);
  multihash[0] = 0x12; multihash[1] = 0x20;
  multihash.set(digest, 2);
  return _base58Encode(multihash);
}

async function _toUint8Array(bytes) {
  if (bytes instanceof Uint8Array)  return bytes;
  if (bytes instanceof ArrayBuffer) return new Uint8Array(bytes);
  return new Uint8Array(await new Response(bytes).arrayBuffer());
}

function _isoNow() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
}

// ─────────────────────────────────────────────────────────────────
// GritsFile
// ─────────────────────────────────────────────────────────────────

export class GritsFile {
  constructor(metaCID, meta, volume) {
    this._metaCID = metaCID;
    this._meta    = meta;
    this._volume  = volume; // GritsVolume, for content fetching
  }

  // Metadata CID — stable node identifier. Use with vol.li().
  cid()        { return this._metaCID; }

  // Content blob CID. Use with vol.get() for raw blob access.
  contentCID() { return this._meta.contentHash; }

  size()       { return this._meta.size; }
  isDir()      { return this._meta.type === 'dir'; }
  isFile()     { return this._meta.type === 'blob'; }
  meta()       { return { ...this._meta }; }

  get()        { return this._volume.get(this._meta.contentHash); }
  async bytes(){ return (await this.get()).arrayBuffer(); }
  async text() { return (await this.get()).text(); }
  async json() { return this._volume.json(this._meta.contentHash); }

  // Returns a Map<name, GritsFile> of this directory's children.
  // Fetches metadata for each child; content is not fetched.
  // Throws if called on a non-directory.
  async children() {
    if (!this.isDir())
      throw new Error('children: not a directory');
    const listing = await this.json();   // { name: metaCID, ... }
    const entries = await Promise.all(
      Object.entries(listing).map(async ([name, metaCID]) => {
        const meta = await this._volume.meta(metaCID);
        return [name, new GritsFile(metaCID, meta, this._volume)];
      })
    );
    return new Map(entries);
  }

  // If this is a directory, fetch the directory listing and return a GritsFile
  // for its index.html entry. Throws if not a directory or no index.html exists.
  async indexHtml() {
    if (!this.isDir())
      throw new Error('indexHtml: not a directory');
    const listing = await this.json();
    const indexCID = listing['index.html'];
    if (!indexCID)
      throw new Error('indexHtml: no index.html in this directory');
    const meta = await this._volume.meta(indexCID);
    return new GritsFile(indexCID, meta, this._volume);
  }

  toString() {
    return `GritsFile(${this._meta.type}, ${this._meta.size}b, cid=${this._metaCID.slice(0,8)}…)`;
  }
}

// ─────────────────────────────────────────────────────────────────
// GritsVolume — server operations + convenience wrappers
// ─────────────────────────────────────────────────────────────────

export class GritsVolume {
  constructor(serverUrl, volume, parent) {
    this._serverUrl = serverUrl.replace(/\/$/, '');
    this._volume    = volume;
    this._parent    = parent; // GritsClient

    // _miniRoots: path → { addr: string|null, ts: number }
    // path "" is the global root (when the server returns it).
    // addr is null when resetRoot() has been called, forcing a server round-trip.
    this._miniRoots = new Map();

    this.hardTimeout = DEFAULT_HARD_TIMEOUT;

    this.serviceWorkerHash = undefined;

    this._configFetched      = false;
    this._inFlightPrefetches = new Map();
    this._prefetchQueue      = [];
    this._isProcessingQueue  = false;

    this.mirrorManager = new MirrorManager({ // %FOR MODULE%
    //self.MirrorManager({                   // %FOR SERVICEWORKER%
      serverUrl: this._serverUrl,
      volume:    this._volume,
      debug:     DEBUG,
    });
    this.mirrorManager.initialize().catch(err =>
      console.error(`[GritsVolume] mirror init (${this._volume}):`, err));
  }

  // ── Lookup ────────────────────────────────────────────────────

  async lookup(path) {
    if (typeof path !== 'string')
      throw new TypeError(`lookup: path must be a string, got ${_typename(path)}`);
    const normalized = path.replace(/^\/+/, '');
    const info = await this._lookup_internal(normalized);
    if (!info) throw new Error(`lookup: ${this._volume}:${path}: not found`);
    const meta = await this._fetchMeta(info.metadataHash);
    return new GritsFile(info.metadataHash, meta, this);
  }

  // Returns a GritsFile for a known metadata CID, without a path lookup.
  async fileForCID(metaCID) {
    _assertString(metaCID, 'fileForCID');
    const meta = await this._fetchMeta(metaCID);
    return new GritsFile(metaCID, meta, this);
  }

  // ── Link ──────────────────────────────────────────────────────

  async link(fileOrCID, path) {
    _assertStringOrFile(fileOrCID, 'link');
    if (typeof path !== 'string')
      throw new TypeError(`link: path must be a string, got ${_typename(path)}`);
    const metaCID = fileOrCID instanceof GritsFile ? fileOrCID.cid() : fileOrCID;
    await this._ensureOnServer(metaCID);
    return this._linkRaw(metaCID, path);
  }

  async multiLink(requests, { maxRetries = 5 } = {}) {
    const url  = `${this._serverUrl}/grits/v1/link/${this._volume}`;
    const body = JSON.stringify(requests.map(r => ({
      path:     _normalizePath(r.path),
      addr:     r.addr     ?? '',
      prevAddr: r.prevAddr ?? '',
      assert:   r.assert   ?? 0,
    })));

    for (let attempt = 0; attempt < maxRetries; attempt++) {
      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body,
      });

      if (resp.ok) {
        const result = await resp.json();
        this._ingestLookupResponse(result);
        return result;
      }

      if (resp.status === 422) {
        const { error, missingAddr } = await resp.json();
        if (error === 'missing_blob') {
          console.log(`[multiLink] server missing ${missingAddr}, uploading...`);
          await this._uploadMissingBlob(missingAddr);
          continue; // retry the link
        }
      }

      const msg = await resp.text().catch(() => resp.statusText);
      if (resp.status === 409) throw new AssertionError(msg);
      throw new Error(`multiLink: ${resp.status} ${msg}`);
    }

    throw new Error(`multiLink: server kept reporting missing blobs after ${maxRetries} attempts`);
  }

  async _uploadMissingBlob(addr) {
    const local = this._parent._local.get(addr);
    if (!local) {
      throw new Error(
        `multiLink: server needs blob ${addr} but it's not in local cache. ` +
        `Did you call vol.mkfile/mkdir to build the tree before linking?`
      );
    }
    await this._uploadBlob(addr, local);
    await this._parent._blobCachePut(addr, new Response(local, { status: 200 }));
    this._parent._local.delete(addr); // FIXME -- Probably this is fine to leave... maybe check size
  }

  // ── Get ───────────────────────────────────────────────────────

  async get(cid) {
    _assertString(cid, 'get');
    const startTime = performance.now();

    const local = this._parent._local.get(cid);
    if (local) return new Response(local, { status: 200 });

    const cached = await this._parent._blobCacheGet(cid);
    if (cached) {
      this._parent._tracker.record('blobCacheHit', performance.now() - startTime);
      return cached;
    }

    const resp = await this._fetchBlob(cid);
    if (resp.ok) {
      this._parent._tracker.record('blobCacheMiss', performance.now() - startTime);
      await this._parent._blobCachePut(cid, resp.clone());
      return resp;
    }
    throw new Error(`get: CID ${cid} not found on ${this._serverUrl}/${this._volume}`);
  }

  // ── Put ───────────────────────────────────────────────────────

  async put(bytes) {
    _assertBytes(bytes, 'put');
    const data = await _toUint8Array(bytes);
    const cid  = await _computeCID(data);
    await this._uploadBlob(cid, data);
    await this._parent._blobCachePut(cid, new Response(data, { status: 200 }));
    return cid;
  }

  // ── Meta / JSON ───────────────────────────────────────────────

  async meta(metaCID) {
    _assertString(metaCID, 'meta');
    return this._fetchMeta(metaCID);
  }

  async json(cid) {
    _assertString(cid, 'json');
    const cached = this._parent._jsonCache.get(cid);
    if (cached) { cached.lastAccessed = Date.now(); return cached.data; }
    const data = await (await this.get(cid)).json();
    this._parent._jsonCache.set(cid, { data, lastAccessed: Date.now() });
    return data;
  }

  // ── mkfile / mkdir / gc — convenience wrappers ───────────────

  mkfile(cid, size) { return this._parent.mkfile(cid, size); }
  mkdir(entries)    { return this._parent.mkdir(entries); }
  gc(cid)           { return this._parent.gc(cid); }

  // ── Misc ──────────────────────────────────────────────────────

  // Force the next lookup to go to the server by nulling all mini-root addrs.
  // The paths are kept so they are still included in the next slow lookup request,
  // ensuring an atomic refresh of everything we care about.
  resetRoot() {
    for (const entry of this._miniRoots.values()) {
      entry.addr = null;
    }
  }

  /** Return the last seen service worker hash for this volume, or undefined. */
  getServiceWorkerHash() { return this.serviceWorkerHash; }

  // ── Internal: server communication ───────────────────────────

  async _uploadBlob(cid, bytes) {
    const resp = await fetch(`${this._serverUrl}/grits/v1/blob/${cid}`, {
      method: 'PUT', body: bytes,
    });
    if (resp.status === 204 || resp.ok) return cid;
    throw new Error(`uploadBlob ${cid}: ${resp.status} ${resp.statusText}`);
  }

  async _linkRaw(metaCID, path) {
    return this.multiLink([{ path, addr: metaCID }]);
  }

  async _fetchBlob(cid) {
    const response = await this.mirrorManager.fetchBlob(cid, null);
    if (response.ok) {
      const result = await this._parent._verifier.verify(response, cid);
      if (!result.ok) {
        console.error(`[GritsVolume] Hash verification FAILED for ${cid}: ${result.error}`);
        this._parent._tracker.count('hashVerifyFail');
        return new Response(
          `Hash verification failed: ${result.error}`,
          { status: 502, headers: { 'Content-Type': 'text/plain' } }
        );
      }
    }
    return response;
  }

  async _fetchMeta(metaCID) {
    const cached = this._parent._jsonCache.get(metaCID);
    if (cached) { cached.lastAccessed = Date.now(); return cached.data; }
    const resp = await this.get(metaCID);
    const data = await resp.json();
    this._parent._jsonCache.set(metaCID, { data, lastAccessed: Date.now() });
    return data;
  }

  async _ensureOnServer(cid, visited = new Set()) {
    if (visited.has(cid)) return;
    visited.add(cid);

    const local = this._parent._local.get(cid);
    if (!local) return;

    try {
      const meta = JSON.parse(new TextDecoder().decode(local));
      if (meta?.contentHash)
        await this._ensureOnServer(meta.contentHash, visited);
    } catch (_) {}

    await this._uploadBlob(cid, local);
    this._parent._local.delete(cid);
    await this._parent._blobCachePut(cid, new Response(local, { status: 200 }));
  }

  // ── Internal: volume config ───────────────────────────────────

  // Reads .grits/volume.json from the volume (using the already-warm root from
  // the just-completed _slowLookup) and applies clientCacheDuration to
  // hardTimeout. Fired once, fire-and-forget, after the first successful
  // _slowLookup — at that point the root is cached so this lookup is local.
  async _fetchVolumeConfig() {
    if (this._configFetched) return;
    this._configFetched = true;
    try {
      const info = await this.lookup_internal('.grits/volume.json');
      if (!info) {
        // file absent — keep default hardTimeout
        this.hardTimeout = DEFAULT_HARD_TIMEOUT;
        return;
      }
      const resp = await this._fetchBlob(info.contentHash);
      if (!resp.ok) return;
      const cfg = await resp.json();
      const ms  = _parseDuration(cfg?.clientCacheDuration);
      if (ms != null && ms > 0) {
        this.hardTimeout = ms;
        DEBUG && console.log(
          `[GritsVolume] ${this._volume}: hardTimeout = ${ms}ms (from .grits/volume.json)`);
      }
    } catch (e) {
      DEBUG && console.warn(
        `[GritsVolume] ${this._volume}: could not load .grits/volume.json:`, e.message);
    }
  }

  // ── Internal: lookup ──────────────────────────────────────────

  // Find a usable mini-root for walking toward `path`.
  // Returns { rootPath, entry } or null if nothing usable.
  // Mini-roots should never be ancestors of each other, so the first
  // match we find is fine.
  _findMiniRoot(path) {
    const now = Date.now();
    for (const [rootPath, entry] of this._miniRoots) {
      if (!entry.addr) continue;
      if (now - entry.ts > this.hardTimeout) continue;
      // "" matches everything; otherwise rootPath must be a prefix of path.
      if (rootPath === '' || path === rootPath || path.startsWith(rootPath + '/')) {
        return { rootPath, entry };
      }
    }
    return null;
  }

  async _lookup_internal(path) {
      const n = _normalizePath(path);
      const abort = new AbortController();

      const slowPromise = this._slowLookup(n).catch(err => {
          abort.abort();
          if (err instanceof AccessDeniedError) {
              // Drop any mini-root we were tracking for this path —
              // we're not allowed to see it.
              this._miniRoots.delete(err.path);
          }
          throw err; // rethrow so lookup() sees it
      });

      const miniRoot = this._findMiniRoot(n);
      if (!miniRoot) return slowPromise;

      const fastPromise = this._fastWalk(n, miniRoot, abort.signal).catch(() =>
          new Promise(() => {}));

      return Promise.race([fastPromise, slowPromise]);
  }

  // Walk down through cached blobs from miniRoot toward path.
  // Resolves with a lookup-info object on success, rejects on any miss or abort.
  async _fastWalk(path, { rootPath, entry }, signal) {
    // Strip the mini-root prefix to get the remaining path segments to walk.
    const remainder = rootPath === ''
      ? path
      : path.slice(rootPath.length + 1); // skip the trailing '/'
    const parts = remainder ? remainder.split('/') : [];

    let metaHash = entry.addr;

    for (const part of parts) {
      if (signal.aborted) throw new DOMException('aborted', 'AbortError');

      const [meta] = await this._parent._unmarshal(metaHash);
      if (!meta) throw new Error('cache miss');
      if (meta.type !== 'dir') throw new Error('not a directory');

      if (signal.aborted) throw new DOMException('aborted', 'AbortError');

      const [dir] = await this._parent._unmarshal(meta.contentHash);
      if (!dir) throw new Error('cache miss');
      if (!dir[part]) throw new Error('not found');

      const childMetaHash = dir[part];
      const [childMeta]   = await this._parent._unmarshal(childMetaHash);
      if (!childMeta) throw new Error('cache miss');

      metaHash = childMetaHash;
    }

    if (signal.aborted) throw new DOMException('aborted', 'AbortError');

    // We need the final meta to return contentHash and size.
    const [finalMeta] = await this._parent._unmarshal(metaHash);
    if (!finalMeta) throw new Error('cache miss');

    this._parent._tracker.record('fastWalkHit', 0);
    return {
      metadataHash: metaHash,
      contentHash:  finalMeta.contentHash,
      contentSize:  finalMeta.size ?? 0,
    };
  }

  async _slowLookup(path) {
    const startTime = performance.now();
    const url = `${this._serverUrl}/grits/v1/lookup`;

    // Build path list: the thing we actually want, plus any live mini-roots
    // (so we get an atomic refresh of everything in one round-trip).
    const now = Date.now();
    const paths = [path];
    for (const [rootPath, entry] of this._miniRoots) {
      if (now - entry.ts < MINIROOT_TTL && rootPath !== path) {
        paths.push(rootPath);
      }
    }

    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ volume: this._volume, paths }),
    });

    if (resp.status === 403) {
        const { path: deniedPath } = await resp.json().catch(() => ({ path }));
        throw new AccessDeniedError(deniedPath);
    }
    if (!resp.ok) return null;

    const swHash = resp.headers.get('X-Grits-Service-Worker-Hash');
    if (swHash) this.serviceWorkerHash = swHash;

    const result = await resp.json();
    this._ingestLookupResponse(result);
    this._updateMiniRoots(path, result);

    const elapsed = performance.now() - startTime;
    this._parent._tracker.record('slowLookup', elapsed);
    this._parent._tracker.trackContentUrl(path, elapsed);

    if (!this._configFetched) this._fetchVolumeConfig();

    const leaf = result.paths?.find(e => e.path === path);
    if (!leaf || result.isPartial) return null;

    return { metadataHash: leaf.addr, contentHash: leaf.contentHash, contentSize: leaf.size ?? 0 };
  }

  // Ingest a lookup/link response: cache all returned blobs and update
  // any mini-root entries whose paths appear in the response.
  _ingestLookupResponse(result) {
    if (!result?.paths?.length) return;

    for (const entry of result.paths) {
      // Update any mini-root we're already tracking.
      if (this._miniRoots.has(entry.path)) {
        const mr = this._miniRoots.get(entry.path);
        mr.addr = entry.addr;
        mr.ts   = Date.now();
      }
    }

    this._startPrefetch(result.paths);
  }

  // After a successful lookup, record the shallowest ancestor we received
  // (above the requested path) as a mini-root for future refreshes.
  _updateMiniRoots(requestedPath, result) {
    if (!result?.paths?.length) return;

    let best = null;
    for (const entry of result.paths) {
      if (entry.path === requestedPath) continue; // target itself, not an ancestor
      // Is this entry an ancestor of (or equal to the root of) requestedPath?
      if (entry.path !== '' && !requestedPath.startsWith(entry.path + '/')) continue;
      // Keep the shallowest (shortest path = highest in tree).
      if (best === null || entry.path.length < best.path.length) {
        best = entry;
      }
    }

    if (best !== null) {
      this._miniRoots.set(best.path, { addr: best.addr, ts: Date.now() });
      DEBUG && console.log(
        `[miniRoot] upsert "${best.path}" → ${best.addr.slice(0, 8)}…`);
    }
  }

  // ── Internal: prefetch ────────────────────────────────────────

  _startPrefetch(paths) {
    for (const e of paths) {
      if (e.addr && !this._inFlightPrefetches.has(e.addr)) {
        this._prefetchQueue.push(e.addr);
        this._inFlightPrefetches.set(e.addr, true);
      }
    }
    if (!this._isProcessingQueue) { this._isProcessingQueue = true; this._drain(); }
  }

  async _drain() {
    while (this._prefetchQueue.length > 0) {
      const hash = this._prefetchQueue.shift();
      try {
        if (!this._parent._jsonCache.has(hash)) {
          const resp = await this._fetchBlob(hash);
          if (!resp.ok) continue;
          const data = await resp.json();
          this._parent._jsonCache.set(hash, { data, lastAccessed: Date.now() });
          this._parent._tracker.count('prefetchSuccess');
          if (data.type === 'dir' && !this._parent._jsonCache.has(data.contentHash)) {
            const r2 = await this._fetchBlob(data.contentHash);
            if (r2.ok) {
              this._parent._jsonCache.set(data.contentHash,
                { data: await r2.json(), lastAccessed: Date.now() });
              this._parent._tracker.count('prefetchSuccess');
            }
          }
        }
      } catch (e) { DEBUG && console.warn(`[prefetch] ${hash}:`, e.message); }
      finally { this._inFlightPrefetches.delete(hash); }
      await new Promise(r => setTimeout(r, 10));
    }
    this._isProcessingQueue = false;
  }
}

// ─────────────────────────────────────────────────────────────────
// GritsClient — cache operations only, no server contact
// ─────────────────────────────────────────────────────────────────

export default class GritsClient {
  constructor() {
    this._local     = new Map(); // cid → Uint8Array  (synthesized, pending upload)
    this._jsonCache = new Map(); // cid → { data, lastAccessed }
    this._blobCache = null;      // browser Cache API
    this._volumes   = new Map(); // volKey → GritsVolume

    this._verifier = new HashVerifier({ debug: DEBUG });
    this._tracker  = new PerformanceTracker({
      enabled:      DEBUG_STATS,
      interval:     10_000,
      mirrorStatsFn: () => this._collectMirrorStats(),
    });

    this._initBlobCache();
    this._cleanupTimer = setInterval(() => this._cleanupJsonCache(), JSON_CACHE_CLEANUP_INTERVAL);
  }

  destroy() {
    clearInterval(this._cleanupTimer);
    this._tracker.destroy();
  }

  /** Access the performance tracker (for custom recording or snapshots). */
  get tracker() { return this._tracker; }

  // ── Volume registration ───────────────────────────────────────

  volume(serverUrl, volumeName) {
    if (typeof serverUrl  !== 'string') throw new TypeError(`volume: serverUrl must be a string, got ${_typename(serverUrl)}`);
    if (typeof volumeName !== 'string') throw new TypeError(`volume: volumeName must be a string, got ${_typename(volumeName)}`);
    const key = _volKey(serverUrl, volumeName);
    if (!this._volumes.has(key))
      this._volumes.set(key, new GritsVolume(serverUrl, volumeName, this));
    return this._volumes.get(key);
  }

  // ── cacheGet ─────────────────────────────────────────────────
  // Read from local or browser cache only. Returns null if not found.

  async cacheGet(cid) {
    _assertString(cid, 'cacheGet');
    const local = this._local.get(cid);
    if (local) return new Response(local, { status: 200 });
    return this._blobCacheGet(cid);
  }

  // ── cachePut ─────────────────────────────────────────────────
  // Store bytes in local cache only. Returns content CID string.

  async cachePut(bytes) {
    _assertBytes(bytes, 'cachePut');
    const data = await _toUint8Array(bytes);
    const cid  = await _computeCID(data);
    if (!this._local.has(cid)) this._local.set(cid, data);
    return cid;
  }

  // ── mkfile ────────────────────────────────────────────────────
  // Synthesize a file metadata blob. Stores in localCache.
  // Returns metadata CID string.

  async mkfile(contentCID, size) {
    _assertString(contentCID, 'mkfile');
    if (typeof size !== 'number' || !Number.isInteger(size) || size < 0)
      throw new TypeError(`mkfile: size must be a non-negative integer, got ${_typename(size)} (${size})`);

    const meta  = {
      type:        'blob',
      size,
      contentHash: contentCID,
      mode:        0o644,
      timestamp:   _isoNow(),
    };
    const bytes = new TextEncoder().encode(JSON.stringify(meta));
    const cid   = await _computeCID(bytes);
    if (!this._local.has(cid)) this._local.set(cid, bytes);
    return cid;
  }

  // ── mkdir ─────────────────────────────────────────────────────
  // Synthesize a directory metadata blob. Stores in localCache.
  // entries: { name: GritsFile|cidString, ... } or null for empty dir.
  // Returns metadata CID string.

  async mkdir(entries = null) {
    _assertEntriesMap(entries, 'mkdir');

    const listing = {};
    for (const [name, value] of Object.entries(entries ?? {})) {
      if (typeof value === 'string') {
        listing[name] = value;
      } else if (value instanceof GritsFile) {
        listing[name] = value.cid();
      } else if (typeof value?.cid === 'function') {
        listing[name] = value.cid();
      } else {
        throw new TypeError(
          `mkdir: entry "${name}" must be a CID string or GritsFile, got ${_typename(value)}`);
      }
    }

    const dirBytes  = new TextEncoder().encode(JSON.stringify(listing));
    const dirCID    = await _computeCID(dirBytes);
    if (!this._local.has(dirCID)) this._local.set(dirCID, dirBytes);

    const meta      = {
      type:        'dir',
      size:        dirBytes.byteLength,
      contentHash: dirCID,
      mode:        0o755,
      timestamp:   _isoNow(),
    };
    const metaBytes = new TextEncoder().encode(JSON.stringify(meta));
    const metaCID   = await _computeCID(metaBytes);
    if (!this._local.has(metaCID)) this._local.set(metaCID, metaBytes);

    return metaCID;
  }

  // ── gc ────────────────────────────────────────────────────────

  gc(cid = null) {
    if (cid !== null) _assertString(cid, 'gc');
    if (cid === null) this._local.clear();
    else this._local.delete(cid);
  }

  // ── Internal: JSON cache (used by GritsVolume) ────────────────

  async _unmarshal(hash) {
    const cached = this._jsonCache.get(hash);
    if (cached) { cached.lastAccessed = Date.now(); return [cached.data, 'memory']; }
    return [null, null];
  }

  _cleanupJsonCache() {
    const cutoff = Date.now() - JSON_CACHE_MAX_AGE;
    for (const [k, v] of this._jsonCache)
      if (v.lastAccessed < cutoff) this._jsonCache.delete(k);
  }

  // ── Internal: browser blob cache (used by GritsVolume) ───────

  async _initBlobCache() {
    try { this._blobCache = await caches.open('grits-blobs-v1'); }
    catch (e) { console.warn('[GritsClient] blob cache unavailable:', e.message); }
  }

  async _blobCacheGet(cid) {
    return this._blobCache?.match(cid).catch(() => null) ?? null;
  }

  async _blobCachePut(cid, resp) {
    if (this._blobCache && resp.ok) this._blobCache.put(cid, resp).catch(() => {});
  }

  // ── Internal: aggregate mirror stats across all volumes ───────

  _collectMirrorStats() {
    const all = [];
    for (const vol of this._volumes.values()) {
      try {
        all.push(...vol.mirrorManager.getMirrorStats());
        vol.mirrorManager.resetStats();
      } catch (_) {}
    }
    return all;
  }
}

function _volKey(serverUrl, volume) {
  return `${serverUrl.replace(/\/$/, '')}|${volume}`;
}

function _normalizePath(path) {
  return path.replace(/^\/+|\/+$/g, '');
}