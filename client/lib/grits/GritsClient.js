// GritsClient.js — Gimbal filesystem client
//
// One instance ("gg") spans multiple hosts and volumes.
//
// Two caches:
//   localCache:  Map<cid, Uint8Array>  — synthesized/pending blobs, lives until gc()
//   blobCache:   browser Cache API     — confirmed server content, can be evicted
//
// Core API (short aliases in parens):
//   gg.put / gg.p  (bytes, serverUrl?, volume?)      → string (content CID)
//   gg.get / gg.g  (cid, serverUrl?, volume?)        → Response
//   gg.lo / gg.lookup (serverUrl, volume, path)      → GritsFile
//   gg.li / gg.link   (file|cid, serverUrl, vol, path) → void
//   gg.mkfile(contentCID ,size)                      → string (metadata CID)
//   gg.mkdir(entries?)                               → string (metadata CID)
//       entries: { name: GritsFile|cidString, ... }
//   gg.meta(metaCID)                                 → { type, size, contentHash, ... }
//   gg.json(contentCID)                              → parsed JS object
//   gg.gc(cid?)                                      → void
//
// Volume handle:
//   const vol = gg.volume(serverUrl, volumeName)
//   All methods available with serverUrl+volume pre-filled.

import MirrorManager from './MirrorManager.js'; // %FOR MODULE%
//importScripts('/grits/v1/content/client/MirrorManager-sw.js'); // %FOR SERVICEWORKER%

const DEBUG = false;
const JSON_CACHE_MAX_AGE          = 5 * 60 * 1000;
const JSON_CACHE_CLEANUP_INTERVAL = 5 * 60 * 1000;

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
  // data must be Uint8Array
  const digest    = new Uint8Array(await crypto.subtle.digest('SHA-256', data));
  const multihash = new Uint8Array(34);
  multihash[0] = 0x12; multihash[1] = 0x20;
  multihash.set(digest, 2);
  return _base58Encode(multihash);
}

async function _toUint8Array(bytes) {
  if (bytes instanceof Uint8Array)  return bytes;
  if (bytes instanceof ArrayBuffer) return new Uint8Array(bytes);
  // Blob, ReadableStream, etc.
  return new Uint8Array(await new Response(bytes).arrayBuffer());
}

function _isoNow() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
}

// ─────────────────────────────────────────────────────────────────
// GritsFile
// ─────────────────────────────────────────────────────────────────

export class GritsFile {
  constructor(metaCID, meta, client) {
    this._metaCID = metaCID;
    this._meta    = meta;
    this._client  = client;
  }

  cid()        { return this._metaCID; }
  contentCID() { return this._meta.contentHash; }
  size()       { return this._meta.size; }
  isDir()      { return this._meta.type === 'dir'; }
  isFile()     { return this._meta.type === 'blob'; }
  meta()       { return { ...this._meta }; }

  get()  { return this._client.get(this._meta.contentHash); }
  json() { return this._client.json(this._meta.contentHash); }
}

// GritsVolume — Unified volume class
// Obtained via gg.volume(serverUrl, volumeName).

export class GritsVolume {
  constructor(serverUrl, volume, parent) {
    this._serverUrl = serverUrl.replace(/\/$/, '');
    this._volume    = volume;
    this._parent    = parent; // GritsClient

    this.rootHash          = null;
    this.rootHashTimestamp = 0;
    this.hardTimeout       = 5 * 60_000;

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
      console.error(`[GritsClient] mirror init (${this._volume}):`, err));
  }

  // ── Public API ────────────────────────────────────────────────

  lo(path)              { return this._parent.lo(this._serverUrl, this._volume, path); }
  lookup(path)          { return this.lo(path); }
  li(fileOrCID, path)   { return this._parent.li(fileOrCID, this._serverUrl, this._volume, path); }
  link(fileOrCID, path) { return this.li(fileOrCID, path); }
  get(cid)              { return this._parent.get(cid, this._serverUrl, this._volume); }
  g(cid)                { return this.get(cid); }
  put(bytes)            { return this._parent.put(bytes, this._serverUrl, this._volume); }
  p(bytes)              { return this.put(bytes); }
  meta(cid)             { return this._parent.meta(cid); }
  json(cid)             { return this._parent.json(cid); }
  mkfile(cid, size)     { return this._parent.mkfile(cid, size); }
  mkdir(entries)        { return this._parent.mkdir(entries); }

  // ── Internal: used by GritsClient ────────────────────────────

  // Upload a single blob. Server returns 204 if already present.
  async uploadBlob(cid, bytes) {
    const resp = await fetch(`${this._serverUrl}/grits/v1/blob/${cid}`, {
      method: 'PUT', body: bytes,
    });
    if (resp.status === 204 || resp.ok) return cid;
    throw new Error(`blob upload ${cid}: ${resp.status} ${resp.statusText}`);
  }

  // Link a metadata CID at a path. Returns the LinkResponse
  // (includes new volume root address).
  async linkRaw(metaCID, path) {
    const url  = `${this._serverUrl}/grits/v1/link/${this._volume}`;
    const body = JSON.stringify([{ path: _normalizePath(path), addr: metaCID }]);
    const resp = await fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body,
    });
    if (!resp.ok) {
      const msg = await resp.text().catch(() => resp.statusText);
      throw new Error(`li ${this._volume}:${path}: ${resp.status} ${msg}`);
    }
    return resp.json(); // { paths: [...], root: newRootCID, ... }
  }

  async fetchBlob(cid) {
    return this.mirrorManager.fetchBlob(cid, null);
  }

  // ── Internal: Merkle tree lookup ─────────────────────────────

  async lookup_internal(path) {
    const n = _normalizePath(path);
    return (await this._tryFastLookup(n)) ?? (await this._slowLookup(n));
  }

  async _tryFastLookup(path) {
    if (!this.rootHash || Date.now() - this.rootHashTimestamp > this.hardTimeout) return null;
    const parts = path.split('/').filter(Boolean);
    let metaHash = this.rootHash, contentHash = null, contentSize = null;
    for (let i = 0; i < parts.length; i++) {
      const [meta] = await this._parent._unmarshal(metaHash);
      if (!meta || meta.type !== 'dir') return null;
      const [dir] = await this._parent._unmarshal(meta.contentHash);
      if (!dir?.[parts[i]]) return null;
      const childMetaHash = dir[parts[i]];
      const [childMeta]   = await this._parent._unmarshal(childMetaHash);
      if (!childMeta) return null;
      metaHash    = childMetaHash;
      contentHash = childMeta.contentHash;
      contentSize = childMeta.size ?? 0;
      if (i === parts.length - 1 && childMeta.type === 'dir') parts.push('index.html');
    }
    return { metadataHash: metaHash, contentHash, contentSize };
  }

  async _slowLookup(path) {
    const url  = `${this._serverUrl}/grits/v1/lookup/${this._volume}`;
    const resp = await fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(path),
    });
    if (!resp.ok) return null;
    const result = await resp.json();
    if (!result.paths?.length) return null;
    const root = result.paths.find(e => e.path === '');
    if (root) { this.rootHash = root.addr; this.rootHashTimestamp = Date.now(); }
    this._startPrefetch(result.paths);
    const leaf = result.paths[result.paths.length - 1];
    return { metadataHash: leaf.addr, contentHash: leaf.contentHash, contentSize: leaf.size ?? 0 };
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
          const resp = await this.fetchBlob(hash);
          if (!resp.ok) continue;
          const data = await resp.json();
          this._parent._jsonCache.set(hash, { data, lastAccessed: Date.now() });
          if (data.type === 'dir' && !this._parent._jsonCache.has(data.contentHash)) {
            const r2 = await this.fetchBlob(data.contentHash);
            if (r2.ok) {
              this._parent._jsonCache.set(data.contentHash,
                { data: await r2.json(), lastAccessed: Date.now() });
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
// GritsClient
// ─────────────────────────────────────────────────────────────────

export default class GritsClient {
  constructor() {
    // localCache: synthesized blobs pending upload. Lives until gc().
    this._local     = new Map(); // cid → Uint8Array

    // jsonCache: parsed metadata/directory JSON. Evicted after idle.
    this._jsonCache = new Map(); // cid → { data, lastAccessed }

    // blobCache: browser Cache API for confirmed server content.
    this._blobCache = null;
    this._initBlobCache();

    this._volumes = new Map(); // volKey → GritsVolume
    this._cleanupTimer = setInterval(() => this._cleanupJsonCache(), JSON_CACHE_CLEANUP_INTERVAL);
  }

  destroy() { clearInterval(this._cleanupTimer); }

  // ── Volume registration ───────────────────────────────────────

  volume(serverUrl, volumeName) {
    const key = _volKey(serverUrl, volumeName);
    if (!this._volumes.has(key))
      this._volumes.set(key, new GritsVolume(serverUrl, volumeName, this));
    return this._volumes.get(key);
  }

  _vol(serverUrl, volume) {
    const v = this._volumes.get(_volKey(serverUrl, volume));
    if (!v) throw new Error(`Unknown volume ${serverUrl}/${volume} — call gg.volume() first`);
    return v;
  }

  // ── put / p ───────────────────────────────────────────────────
  // Store bytes. Local-only if no server args.
  // With server args: upload to server (dedup via hash), store in blobCache.
  // Does NOT put in localCache if uploading to server — server is the backing store.
  // Returns content CID string.

  async put(bytes, serverUrl = null, volume = null) {
    const data = await _toUint8Array(bytes);
    const cid  = await _computeCID(data);

    if (serverUrl && volume) {
      // Upload to server; cache the response in blobCache
      await this._vol(serverUrl, volume).uploadBlob(cid, data);
      // Store in blobCache so get() can find it without a round-trip
      const synthetic = new Response(data, { status: 200 });
      await this._blobCachePut(cid, synthetic);
    } else {
      // Local only — keep in localCache until li() flushes it
      if (!this._local.has(cid)) this._local.set(cid, data);
    }

    return cid;
  }

  p(bytes, serverUrl = null, volume = null) { return this.put(bytes, serverUrl, volume); }

  // ── get / g ───────────────────────────────────────────────────
  // Fetch content by CID.
  // Checks: localCache → blobCache → server (if args given, or tries all volumes)
  // Returns a Response.

  async get(cid, serverUrl = null, volume = null) {
    // 1. local synthetic cache
    const local = this._local.get(cid);
    if (local) return new Response(local, { status: 200 });

    // 2. browser blob cache
    const cached = await this._blobCacheGet(cid);
    if (cached) return cached;

    // 3. specific volume
    if (serverUrl && volume) {
      const resp = await this._vol(serverUrl, volume).fetchBlob(cid);
      if (resp.ok) { await this._blobCachePut(cid, resp.clone()); return resp; }
      throw new Error(`get: CID ${cid} not found on ${serverUrl}/${volume}`);
    }

    throw new Error(`get: CID ${cid} not found`);
  }

  g(cid, serverUrl = null, volume = null) { return this.get(cid, serverUrl, volume); }

  // ── mkfile ────────────────────────────────────────────────────
  // Synthesize a file metadata blob from a content CID.
  // Size is inferred from localCache; must be in localCache or provided.
  // Stores result in localCache. Returns metadata CID string.

  async mkfile(contentCID, size) {
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
  // Synthesize a directory metadata blob.
  // entries: { name: GritsFile|cidString, ... } or omit for empty dir.
  // Stores content listing blob + metadata blob in localCache.
  // Returns metadata CID string.

  async mkdir(entries = null) {
    // Normalize entries — accept GritsFile, cid string, or anything with .cid()
    const listing = {};
    for (const [name, value] of Object.entries(entries ?? {})) {
      if (typeof value === 'string') {
        listing[name] = value;
      } else if (value instanceof GritsFile) {
        listing[name] = value.cid();
      } else if (typeof value?.cid === 'function') {
        listing[name] = value.cid();
      } else {
        throw new TypeError(`mkdir: entry "${name}" is not a CID string or GritsFile`);
      }
    }

    const dirBytes = new TextEncoder().encode(JSON.stringify(listing));
    const dirCID   = await _computeCID(dirBytes);
    if (!this._local.has(dirCID)) this._local.set(dirCID, dirBytes);

    const meta     = {
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

  // ── lo / lookup ───────────────────────────────────────────────
  // Lookup path → GritsFile (identified by metadata CID)

  async lo(serverUrl, volume, path) {
    const info = await this._vol(serverUrl, volume).lookup_internal(path);
    if (!info) throw new Error(`${volume}:${path}: not found`);
    const meta = await this._fetchMeta(info.metadataHash, serverUrl, volume);
    return new GritsFile(info.metadataHash, meta, this);
  }

  lookup(serverUrl, volume, path) { return this.lo(serverUrl, volume, path); }

  // ── li / link ─────────────────────────────────────────────────
  // Link a node at a path. Accepts GritsFile or metadata CID string.
  // Uploads content blob first, then metadata blob, then links.

  async li(fileOrCID, serverUrl, volume, path) {
    const metaCID = fileOrCID instanceof GritsFile ? fileOrCID.cid() : fileOrCID;
    await this._ensureOnServer(metaCID, serverUrl, volume);
    return this._vol(serverUrl, volume).link(metaCID, path);
  }

  link(fileOrCID, serverUrl, volume, path) { return this.li(fileOrCID, serverUrl, volume, path); }

  // ── meta ──────────────────────────────────────────────────────

  async meta(metaCID) { return this._fetchMeta(metaCID, null, null); }

  // ── json ──────────────────────────────────────────────────────

  async json(cid) {
    const cached = this._jsonCache.get(cid);
    if (cached) { cached.lastAccessed = Date.now(); return cached.data; }
    const data = await (await this.get(cid)).json();
    this._jsonCache.set(cid, { data, lastAccessed: Date.now() });
    return data;
  }

  // ── gc ────────────────────────────────────────────────────────

  gc(cid = null) {
    if (cid === null) this._local.clear();
    else this._local.delete(cid);
  }

  // ── Internal: upload local blobs to server (content first) ───

  async _ensureOnServer(cid, serverUrl, volume, visited = new Set()) {
    if (visited.has(cid)) return;
    visited.add(cid);

    const local = this._local.get(cid);
    if (!local) return; // not local — assume server has it

    // If this looks like a metadata blob, upload its content blob first
    try {
      const meta = JSON.parse(new TextDecoder().decode(local));
      if (meta?.contentHash) {
        await this._ensureOnServer(meta.contentHash, serverUrl, volume, visited);
      }
    } catch (_) { /* plain content blob, not JSON */ }

    // Now upload this blob
    await this._vol(serverUrl, volume).uploadBlob(cid, local);

    // Move from localCache to blobCache now that server has it
    this._local.delete(cid);
    await this._blobCachePut(cid, new Response(local, { status: 200 }));
  }

  // ── Internal: fetch and parse metadata ───────────────────────

  async _fetchMeta(metaCID, serverUrl, volume) {
    const cached = this._jsonCache.get(metaCID);
    if (cached) { cached.lastAccessed = Date.now(); return cached.data; }
    const resp = await this.get(metaCID, serverUrl, volume);
    const data = await resp.json();
    this._jsonCache.set(metaCID, { data, lastAccessed: Date.now() });
    return data;
  }

  // ── Internal: JSON cache (used by VolumeClient fast path) ────

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

  // ── Internal: browser blob cache ─────────────────────────────

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
}

function _volKey(serverUrl, volume) {
  return `${serverUrl.replace(/\/$/, '')}|${volume}`;
}

function _normalizePath(path) {
  return path.replace(/^\/+|\/+$/g, '');
}
