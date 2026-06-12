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
//   vol.getServiceWorkerHash()            → string | null | undefined
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
//importScripts('/grits-MirrorManager-sw.js');      // %FOR SERVICEWORKER%
//importScripts('/grits-HashVerifier-sw.js');        // %FOR SERVICEWORKER%
//importScripts('/grits-PerformanceTracker-sw.js');  // %FOR SERVICEWORKER%

const DEBUG       = false;
const DEBUG_STATS = true;
const JSON_CACHE_MAX_AGE          = 5 * 60 * 1000;
const JSON_CACHE_CLEANUP_INTERVAL = 5 * 60 * 1000;
const DEFAULT_HARD_TIMEOUT        = 1 * 60 * 1000; // 1 minute, matches Go-side default
const MINIROOT_TTL                = 1 * 60 * 1000; // 1 minute

// When true, link() returns immediately and flushes to the server in the background.
// Lookups consult a local override map so reads see writes immediately.
const DESYNC_MODE = false;

// True when a SW is controlling this window context. Starts false, switches
// permanently to true the first time a lookup response carries X-Grits-Served-By: sw.
// In this mode GritsClient skips all local caching and passes fetches straight
// through, letting the SW be the single cache layer.
let SW_CONTROLLED = false;

// ─────────────────────────────────────────────────────────────────
// MultiLink assertion flags — mirror of Go-side constants in namestore.go
// ─────────────────────────────────────────────────────────────────

const ASSERT_PREV_MATCHES = 1;
const ASSERT_IS_BLOB      = 2;
const ASSERT_IS_TREE      = 4;
const ASSERT_IS_NONEMPTY  = 8;

class AssertionError extends Error {
  constructor(msg) {
    super(msg);
    this.name = 'AssertionError';
  }
}

class AccessDeniedError extends Error {
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

// Deep-freeze an object/array tree so it cannot be mutated.
// Uses a Set to handle circular references safely.
// Primitives and null/undefined pass through unchanged.
function _deepFreeze(obj, seen = new Set()) {
  if (obj === null || obj === undefined || typeof obj !== 'object') return obj;
  if (seen.has(obj)) return obj;
  seen.add(obj);
  Object.freeze(obj);
  for (const key of Object.getOwnPropertyNames(obj)) {
    _deepFreeze(obj[key], seen);
  }
  return obj;
}

// ─────────────────────────────────────────────────────────────────
// GritsFile
// ─────────────────────────────────────────────────────────────────

class GritsFile {
  constructor(metaCID, meta, volume, path = null) {
    this._metaCID = metaCID;
    this._meta    = meta;
    this._volume  = volume; // GritsVolume, for content fetching
    this._path    = path;   // normalized path, set when obtained via lookup()
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
    // In desync mode, wait for any pending writes inside this directory to flush
    // before fetching the listing, so callers see a consistent view.
    if (this._path) await this._volume._waitForDescendants(this._path);
    const listing = await this.json();   // { name: metaCID, ... }
    const entries = await Promise.all(
      Object.entries(listing).map(async ([name, metaCID]) => {
        const meta = await this._volume.meta(metaCID);
        const childPath = this._path ? `${this._path}/${name}` : null;
        return [name, new GritsFile(metaCID, meta, this._volume, childPath)];
      })
    );
    return new Map(entries);
  }

  // If this is a directory, fetch the directory listing and return a GritsFile
  // for its index.html entry. Throws if not a directory or no index.html exists.
  async indexHtml() {
    if (!this.isDir())
      throw new Error('indexHtml: not a directory');
    // Same desync wait as children().
    if (this._path) await this._volume._waitForDescendants(this._path);
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
// DesyncQueue — background link flusher for DESYNC_MODE
//
// Maintains an ordered list of pending override entries:
//   { path, addr, assert, prevAddr, seq }
//
// _overrides is a Map<path, entry> holding the *most recent* override
// for each path. It is the authoritative local view for lookups.
//
// _queue is the FIFO list of all pending entries waiting to be flushed.
// Entries in _queue that have been superseded (their path's current
// override seq no longer matches their own seq) are skipped when dequeued.
// ─────────────────────────────────────────────────────────────────

class DesyncQueue {
  constructor(volume) {
    this._volume    = volume; // GritsVolume back-reference
    this._overrides = new Map();  // path → { addr, assert, prevAddr, seq }
    this._queue     = [];         // [{ path, addr, assert, prevAddr, seq }, ...]
    this._seq       = 0;
    this._flushing  = false;
    this._waiters   = new Map();  // seq → [{ resolve, reject }, ...]
  }

  // ── Override map queries (used by lookup) ─────────────────────

  // Find the most recently enqueued override whose path is a proper ancestor
  // of (or exactly equal to) `targetPath`. Returns { path, addr, seq } or null.
  findAncestorOverride(targetPath) {
    let best = null;
    for (const [path, entry] of this._overrides) {
      if (!_isAncestorOrSelf(path, targetPath)) continue;
      if (best === null || entry.seq > best.seq) {
        best = { path, addr: entry.addr, seq: entry.seq };
      }
    }
    if (best) {
      console.log(`[desync] findAncestorOverride("${targetPath}") → override at "${best.path}" addr=${best.addr?.slice(0,8)}… seq=${best.seq}`);
    }
    return best;
  }

  // Find the highest seq among all pending overrides that are proper descendants
  // of (or exactly equal to) `targetPath`. Returns the seq number, or -1 if none.
  findDescendantSeq(targetPath) {
    let highestSeq = -1;
    for (const [path, entry] of this._overrides) {
      if (!_isAncestorOrSelf(targetPath, path)) continue;
      if (entry.seq > highestSeq) highestSeq = entry.seq;
    }
    if (highestSeq >= 0) {
      console.log(`[desync] findDescendantSeq("${targetPath}") → must wait for seq=${highestSeq}`);
    }
    return highestSeq;
  }

  // Return a promise that resolves (with the flush result) or rejects when
  // seq `targetSeq` has been processed (successfully or not).
  waitForSeq(targetSeq) {
    return new Promise((resolve, reject) => {
      if (!this._waiters.has(targetSeq)) this._waiters.set(targetSeq, []);
      this._waiters.get(targetSeq).push({ resolve, reject });
    });
  }

  // ── Enqueueing ────────────────────────────────────────────────

  // Enqueue a link operation. Returns immediately.
  // `addr` may be null/'' to represent an unlink.
  enqueue(path, addr, assert = 0, prevAddr = '') {
    const seq = ++this._seq;
    const entry = { addr, assert, prevAddr, seq };
    this._overrides.set(path, entry);
    this._queue.push({ path, addr, assert, prevAddr, seq });
    console.log(`[desync] enqueue seq=${seq} path="${path}" addr=${addr?.slice(0,8) ?? 'null'}… queue depth=${this._queue.length}`);
    this._kick();
  }

  // ── Background flush ──────────────────────────────────────────

  _kick() {
    if (!this._flushing) {
      console.log(`[desync] flush worker starting (queue depth=${this._queue.length})`);
      this._flushing = true;
      this._flush().catch(err => {
        // Should never reach here since _flush handles its own errors,
        // but guard against _flushing staying true forever if something escapes.
        console.error(`[desync] unexpected error escaping _flush:`, err);
        this._flushing = false;
      });
    }
  }

  _notifyWaiters(seq, result, err) {
    const waiters = this._waiters.get(seq);
    if (!waiters) return;
    this._waiters.delete(seq);
    for (const { resolve, reject } of waiters) {
      if (err) reject(err);
      else resolve(result);
    }
  }

  async _flush() {
    while (this._queue.length > 0) {
      const item = this._queue[0]; // peek, don't shift yet

      // Skip if this entry has been superseded by a later enqueue for the same path.
      const current = this._overrides.get(item.path);
      if (!current || current.seq !== item.seq) {
        this._queue.shift();
        console.log(`[desync] skipping superseded seq=${item.seq} for "${item.path}" (current seq=${current?.seq ?? 'gone'})`);
        this._notifyWaiters(item.seq, null, null); // superseded — resolve with null
        continue;
      }

      console.log(`[desync] flushing seq=${item.seq} path="${item.path}" addr=${item.addr?.slice(0,8) ?? 'null'}…`);

      try {
        // Call _serverMultiLink directly to bypass the desync interception in multiLink().
        const result = await this._volume._serverMultiLink([{
          path:     item.path,
          addr:     item.addr     ?? '',
          prevAddr: item.prevAddr ?? '',
          assert:   item.assert   ?? 0,
        }]);

        this._queue.shift(); // success — now remove from queue

        // Remove from override map only if still the current entry.
        if (this._overrides.get(item.path)?.seq === item.seq) {
          this._overrides.delete(item.path);
          console.log(`[desync] flushed seq=${item.seq} path="${item.path}" — override cleared`);
        } else {
          console.log(`[desync] flushed seq=${item.seq} path="${item.path}" — override already superseded, leaving`);
        }

        this._notifyWaiters(item.seq, result, null);

      } catch (err) {
        if (err instanceof AssertionError) {
          // Assertion failed on server — drop the entry and warn the user.
          this._queue.shift();
          if (this._overrides.get(item.path)?.seq === item.seq) {
            this._overrides.delete(item.path);
          }
          console.warn(
            `[Grits] Background link assertion failed for "${item.path}" — ` +
            `the change was not saved. You may need to retry your operation.\n` +
            `Detail: ${err.message}`
          );
          this._notifyWaiters(item.seq, null, null); // assertion fail — resolve with null (drop)
        } else if (err.message?.includes('file does not exist')) {
          // Permanent server error — parent path was deleted before this child
          // could be written (e.g. rmdir race). Drop silently; the parent unlink
          // already cleaned up the subtree.
          this._queue.shift();
          if (this._overrides.get(item.path)?.seq === item.seq) {
            this._overrides.delete(item.path);
          }
          console.warn(`[desync] dropping seq=${item.seq} path="${item.path}" — parent no longer exists on server`);
          this._notifyWaiters(item.seq, null, null);
        } else {
          // Transient error — leave item at front of queue and pause before retrying.
          console.error(`[desync] transient error flushing seq=${item.seq} path="${item.path}", retrying in 2s:`, err);
          await new Promise(r => setTimeout(r, 2_000));
        }
      }
    }
    console.log(`[desync] flush worker done, queue empty`);
    this._flushing = false;
  }
}

// Returns true if `ancestorPath` is a prefix ancestor of (or identical to) `targetPath`.
// Both paths should already be normalized (no leading/trailing slashes).
function _isAncestorOrSelf(ancestorPath, targetPath) {
  if (ancestorPath === targetPath) return true;
  if (ancestorPath === '') return true; // root covers everything
  return targetPath.startsWith(ancestorPath + '/');
}

// ─────────────────────────────────────────────────────────────────
// GritsVolume — server operations + convenience wrappers
// ─────────────────────────────────────────────────────────────────

class GritsVolume {
  constructor(serverUrl, volume, parent) {
    this._serverUrl = serverUrl.replace(/\/$/, '');
    this._volume    = volume;
    this._parent    = parent; // GritsClient

    // _miniRoots: path → { addr: string|null, ts: number }
    // path "" is the global root (when the server returns it).
    // addr is null when resetRoot() has been called, forcing a server round-trip.
    this._miniRoots = new Map();

    this.hardTimeout = DEFAULT_HARD_TIMEOUT;

    this._configFetched      = false;
    this._inFlightPrefetches = new Map();
    this._prefetchQueue      = [];
    this._isProcessingQueue  = false;

    this.mirrorManager = new MirrorManager({
      serverUrl: this._serverUrl,
      volume:    this._volume,
      debug:     DEBUG,
    });
    this.mirrorManager.initialize().catch(err =>
      console.error(`[GritsVolume] mirror init (${this._volume}):`, err));

    // Desync support — only allocated when DESYNC_MODE is on.
    this._desync = DESYNC_MODE ? new DesyncQueue(this) : null;
  }

  // ── SW hash ───────────────────────────────────────────────────

  // Returns the last SW hash seen from this volume's server:
  //   undefined — no server contact yet
  //   null      — server responded but header was absent (SW module not installed)
  //   string    — hash value
  getServiceWorkerHash() {
    return this._parent._serviceWorkerHashes.get(this._serverUrl);
  }

  _updateServiceWorkerHash(resp) {
    const serverHash = resp.headers.get('X-Grits-Sw-Hash');
    const swControlled = resp.headers.get('X-Grits-SW-Controlled') === '1';
    const swSelfHash = resp.headers.get('X-Grits-SW-Self-Hash');

    // Record what the server thinks its current SW hash is.
    //   undefined  never heard from this server
    //   null       server responded but SW module not installed
    //   string     server's current SW dir hash
    this._parent._serviceWorkerHashes.set(this._serverUrl, serverHash ?? null);

    // All of the behavior below only makes sense in a window context.
    if (typeof window === 'undefined') return;

    DEBUG && console.log(
      `[GritsVolume:${this._serverUrl}] SW hash update: ` +
      `server=${serverHash ?? '(absent)'} controlled=${swControlled} ` +
      `self=${swSelfHash ?? '(absent)'}`
    );

    // Case 1: response came from a SW, and that SW's baked-in hash doesn't
    // match the server's current hash. We reload once — the SW will detect
    // the mismatch on its own navigate event and redirect us through the
    // interstitial, which registers a fresh SW.
    if (swControlled && swSelfHash && serverHash && swSelfHash !== serverHash) {
      if (!this._parent._swReloadTriggered) {
        this._parent._swReloadTriggered = true;
        console.log(
          `[GritsVolume:${this._serverUrl}] stale SW detected ` +
          `(self=${swSelfHash} server=${serverHash}) — reloading page`
        );
        window.location.reload();
      }
      return;
    }

    // Case 2: response came direct from the server, server has a SW module,
    // but nothing is controlling this page. Register the SW so it's ready
    // for the next navigation. Doesn't affect this page.
    if (!swControlled && serverHash) {
      const hasCooldown = document.cookie.split(';').some(c =>
        c.trim().startsWith('grits-sw-loading='));
      if (hasCooldown) {
        DEBUG && console.log(
          `[GritsVolume:${this._serverUrl}] cooldown cookie present, skipping SW registration`
        );
        return;
      }
      // Only try once per client lifetime.
      if (this._parent._swRegistrationAttempted) return;
      this._parent._swRegistrationAttempted = true;

      console.log(`[GritsVolume:${this._serverUrl}] SW available but not controlling, registering`);
      document.cookie = 'grits-sw-loading=1; path=/; max-age=30; SameSite=Lax';
      navigator.serviceWorker.register('/grits-serviceworker.js').catch(err =>
        console.warn(`[GritsVolume:${this._serverUrl}] SW registration failed:`, err));
      return;
    }

    // Case 3: nothing to do. Either the server has no SW, or SW is
    // controlling and the hashes match, or we've already triggered a reload.
  }

  // ── Server headers helper ─────────────────────────────────────

  // Returns headers for outgoing server requests, merging any extraHeaders
  // set on the parent GritsClient. Used by the SW test harness to forward
  // hash override headers onto lookup/link/upload requests.
  _serverHeaders(extra = {}) {
    return { ...this._parent.extraHeaders, ...extra };
  }

  // ── Lookup ────────────────────────────────────────────────────

  async lookup(path) {
    if (typeof path !== 'string')
      throw new TypeError(`lookup: path must be a string, got ${_typename(path)}`);
    const normalized = path.replace(/^\/+/, '');
    const displayPath = `//${this._volume}/${normalized}`;
    DEBUG && console.debug(`[GritsClient] lookup("${path}") normalized="${normalized}"`);
    const info = await this._lookup_internal(normalized);
    if (!info) {
      DEBUG && console.debug(`[GritsClient] lookup → info is null, throwing "not found" for ${displayPath}`);
      throw new Error(`lookup: ${displayPath}: not found`);
    }
    DEBUG && console.debug(`[GritsClient] lookup → info=${JSON.stringify(info)}, fetching meta`);
    const meta = await this._fetchMeta(info.metadataHash);
    DEBUG && console.debug(`[GritsClient] lookup → "${normalized}" metaCID=${info.metadataHash} contentCID=${meta?.contentHash} type=${meta?.type} size=${meta?.size}`);
    return new GritsFile(info.metadataHash, meta, this, normalized);
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
    DEBUG && console.log(`[link] metaCID=${metaCID} path="${_normalizePath(path)}" volume=${this._volume}`);
    return this._linkRaw(metaCID, path);
  }

  async multiLink(requests) {
    if (DESYNC_MODE) {
      return this._desyncMultiLink(requests);
    }
    return this._serverMultiLink(requests);
  }

  // Direct-to-server link path. Used by the background flush and by multiLink
  // when DESYNC_MODE is off. Never intercepted by desync logic.
  async _serverMultiLink(requests) {
    const url  = `${this._serverUrl}/grits/v1/link`;
    const body = JSON.stringify({
      volume:   this._volume,
      requests: requests.map(r => ({
        path:     _normalizePath(r.path),
        addr:     r.addr     ?? '',
        prevAddr: r.prevAddr ?? '',
        assert:   r.assert   ?? 0,
      })),
    });

    DEBUG && console.log(`[multiLink] POST ${url} volume=${this._volume} requests=${JSON.stringify(requests.map(r => ({path: _normalizePath(r.path), addr: r.addr})))}`);

    const seenMissing = new Set();
    while (true) {
      const resp = await fetch(url, {
        method:  'POST',
        headers: this._serverHeaders({ 'Content-Type': 'application/json' }),
        body,
      });

      this._updateServiceWorkerHash(resp);

      if (resp.ok) {
        const result = await resp.json();
        DEBUG && console.log(`[multiLink] response paths=${JSON.stringify(result.paths?.map(p => ({path: p.path, addr: p.addr, size: p.size, error: p.error})))}`);
        this._ingestLookupResponse(result);
        return result;
      }

      if (resp.status === 422) {
        const { error, missingAddr } = await resp.json();
        if (error === 'missing_blob') {
          if (seenMissing.has(missingAddr)) {
            throw new Error(`multiLink: stuck, server repeatedly requested missing blob ${missingAddr}`);
          }
          seenMissing.add(missingAddr);
          DEBUG && console.log(`[multiLink] server missing ${missingAddr}, uploading...`);
          await this._uploadMissingBlob(missingAddr);
          continue; // retry the link
        }
      }

      const msg = await resp.text().catch(() => resp.statusText);
      if (resp.status === 409) throw new AssertionError(msg);
      throw new Error(`multiLink: ${resp.status} ${msg}`);
    }

    // unreachable: loop exits only via success or error
  }

  // Desync path for multiLink: validate assertions locally, enqueue each
  // request, then do a real lookup so the caller gets a proper full-path
  // response (with correct ancestor chain for miniroot updates etc.).
  async _desyncMultiLink(requests) {
    for (const r of requests) {
      const path     = _normalizePath(r.path);
      const addr     = r.addr     ?? '';
      const prevAddr = r.prevAddr ?? '';
      const assert   = r.assert   ?? 0;

      // If assertions are requested, evaluate them locally before enqueuing.
      if (assert !== 0) {
        console.log(`[desync] checking assertions for "${path}" assert=${assert}`);
        let currentFile = null;
        try { currentFile = await this.lookup(path); } catch (_) {}
        const assertErr = _checkAssertions(assert, prevAddr, currentFile);
        if (assertErr) {
          console.warn(`[desync] local assertion failed for "${path}": ${assertErr}`);
          throw new AssertionError(assertErr);
        }
        console.log(`[desync] assertions passed for "${path}"`);
      }

      this._desync.enqueue(path, addr, assert, prevAddr);
    }

    // Do a real lookup of the first (usually only) path now that the override
    // is in the map. _lookup_internal will route through _desyncLookup and
    // return a proper result with a full ancestor chain.
    const primaryPath = _normalizePath(requests[0].path);
    console.log(`[desync] post-enqueue lookup for "${primaryPath}"`);
    const info = await this._lookup_internal(primaryPath);
    if (!info) {
      console.log(`[desync] post-enqueue lookup for "${primaryPath}" returned null (unlink?)`);
      return { paths: [] };
    }
    console.log(`[desync] post-enqueue lookup for "${primaryPath}" → ${info.metadataHash?.slice(0,8)}…`);
    return { paths: [{ path: primaryPath, addr: info.metadataHash, size: info.contentSize }] };
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

    if (this._parent._swControlled) {
      return fetch(`${this._serverUrl}/grits/v1/blob/${cid}`);
    }

    // If we've heard from this server and it has a SW module installed, let the
    // SW handle blob fetches rather than caching them ourselves.
    const swHash = this.getServiceWorkerHash();
    if (swHash !== null && swHash !== undefined) {
      return fetch(`${this._serverUrl}/grits/v1/blob/${cid}`);
    }

    const startTime = performance.now();

    const local = this._parent._local.get(cid);
    if (local) { DEBUG && console.log(`[get] CID=${cid} source=local`); return new Response(local, { status: 200 }); }

    const cached = await this._parent._blobCacheGet(cid);
    if (cached) {
      this._parent._tracker.record('blobCacheHit', performance.now() - startTime);
      DEBUG && console.log(`[get] CID=${cid} source=blobCache`);
      return cached;
    }

    const resp = await this._fetchBlob(cid);
    if (resp.ok) {
      this._parent._tracker.record('blobCacheMiss', performance.now() - startTime);
      await this._parent._blobCachePut(cid, resp.clone());
      DEBUG && console.log(`[get] CID=${cid} source=network`);
      return resp;
    }
    throw new Error(`get: CID ${cid} not found on ${this._serverUrl}/${this._volume}`);
  }

  // ── Put ───────────────────────────────────────────────────────

  async _serverHasBlob(cid) {
    try {
      const resp = await fetch(`${this._serverUrl}/grits/v1/blob/${cid}`, {
        method:  'HEAD',
        headers: this._serverHeaders(),
      });
      return resp.ok;            // 200 present, 404 absent
    } catch {
      return false;              // network error → don't know → fall through and upload
    }
  }

  async put(bytes) {
    _assertBytes(bytes, 'put');
    const data = await _toUint8Array(bytes);
    const cid  = await _computeCID(data);
    // Shell-upload path only. Cheap existence check before pushing bytes so a slow
    // uplink doesn't re-send a blob the server already holds. Deliberately NOT in
    // _uploadBlob: the link-fails path already knows the server lacks the blob (it
    // got a 422), so a HEAD there would be a guaranteed-wasted round-trip.
    if (!(await this._serverHasBlob(cid))) {
      DEBUG && console.log(`[put] uploading CID=${cid} (${data.length} bytes)`);
      await this._uploadBlob(cid, data);
    } else {
      DEBUG && console.log(`[put] CID=${cid} already on server`);
    }
    await this._parent._blobCachePut(cid, new Response(data, { status: 200 }));
    DEBUG && console.log(`[put] CID=${cid} cached locally, returning`);
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
    if (cached) {
      cached.lastAccessed = Date.now();
      DEBUG && console.log(`[json] CID=${cid} cache=hit data=${JSON.stringify(cached.data).slice(0,200)}`);
      return cached.data;
    }
    
    DEBUG && console.log(`[json] CID=${cid} cache=miss — fetching`);
    const resp = await this.get(cid);
    const data = await resp.json();
    DEBUG && console.log(`[json] CID=${cid} fetched data=${JSON.stringify(data).slice(0,200)}`);

    this._parent._jsonCache.set(cid, { data: _deepFreeze(data), lastAccessed: Date.now() });
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

  // In desync mode, wait until all pending writes whose path is at or below
  // `dirPath` have been flushed to the server. Used by children() and indexHtml()
  // so directory listings reflect locally-committed writes.
  async _waitForDescendants(dirPath) {
    if (!DESYNC_MODE || !this._desync) return;
    const seq = this._desync.findDescendantSeq(dirPath);
    if (seq < 0) return;
    console.log(`[desync] _waitForDescendants("${dirPath}") waiting for seq=${seq}`);
    await this._desync.waitForSeq(seq);
    console.log(`[desync] _waitForDescendants("${dirPath}") done`);
  }

  // ── Internal: server communication ───────────────────────────

  async _uploadBlob(cid, bytes) {
    let resp;
    try {
      resp = await fetch(`${this._serverUrl}/grits/v1/blob/${cid}`, {
        method:  'PUT',
        headers: this._serverHeaders(),
        body:    bytes,
      });
    } catch (err) {
      // A reset / "Failed to fetch" could be a false negative: the server had
      // the blob and dropped the connection on its dedup early-return, or committed
      // it just before the reset. Confirm absence before reporting failure.
      if (await this._serverHasBlob(cid)) return cid;
      throw err;
    }
    if (resp.status === 204 || resp.ok) return cid;
    throw new Error(`uploadBlob ${cid}: ${resp.status} ${resp.statusText}`);
  }

  async _linkRaw(metaCID, path) {
    DEBUG && console.log(`[_linkRaw] metaCID=${metaCID} path="${_normalizePath(path)}"`);
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
    if (cached) {
      cached.lastAccessed = Date.now();
      DEBUG && console.log(`[_fetchMeta] CID=${metaCID} cache=hit type=${cached.data?.type} size=${cached.data?.size}`);
      return cached.data;
    }
    DEBUG && console.log(`[_fetchMeta] CID=${metaCID} cache=miss — fetching`);
    const resp = await this.get(metaCID);
    const data = await resp.json();
    DEBUG && console.log(`[_fetchMeta] CID=${metaCID} fetched type=${data?.type} size=${data?.size}`);
    this._parent._jsonCache.set(metaCID, { data: _deepFreeze(data), lastAccessed: Date.now() });
    return data;
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
      const info = await this._lookup_internal('.grits/volume.json');
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
        DEBUG && console.log(`[findMiniRoot] MATCH path="${path}" rootPath="${rootPath}" addr=${entry.addr?.slice(0,8)}… age=${now - entry.ts}ms`);
        return { rootPath, entry };
      }
    }
    DEBUG && console.log(`[findMiniRoot] NO MATCH for path="${path}"`);
  }

  async _lookup_internal(path) {
    const n = _normalizePath(path);
    DEBUG && console.debug(`[GritsClient] _lookup_internal("${path}") normalized="${n}" miniRoots=[${[...this._miniRoots.keys()].join(',')}]`);

    if (this._parent._swControlled) {
      DEBUG && console.debug(`[GritsClient] _lookup_internal → SW controlled, slow path only`);
      return this._slowLookup(n);
    }

    if (DESYNC_MODE && this._desync) {
      // ── Ancestor override: we know better than the server ──────
      // Use our local override CID as the starting point and walk down.
      const override = this._desync.findAncestorOverride(n);
      if (override) {
        console.log(`[desync] lookup "${n}" routing via override at "${override.path}"`);
        return this._desyncLookup(n, override);
      }

      // ── Descendant override: server is behind us ───────────────
      // There's a pending write somewhere below the path we're looking up.
      // The server's directory CID for `n` would be stale (wrong Merkle
      // commitment), so we must wait for all descendants to flush first,
      // then ask the server for a fresh answer.
      const descendantSeq = this._desync.findDescendantSeq(n);
      if (descendantSeq >= 0) {
        console.log(`[desync] lookup "${n}" waiting for descendant seq=${descendantSeq} to flush`);
        await this._desync.waitForSeq(descendantSeq);
        console.log(`[desync] lookup "${n}" descendant seq=${descendantSeq} flushed, proceeding with server lookup`);
        // Fall through to normal server lookup below — no desync routing.
      }
    }

    // ── Normal path ───────────────────────────────────────────
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
    if (!miniRoot) {
      DEBUG && console.debug(`[GritsClient] _lookup_internal → no miniRoot for "${n}", relying on slowLookup`);
      return slowPromise;
    }
    DEBUG && console.debug(`[GritsClient] _lookup_internal → miniRoot found for "${n}": rootPath="${miniRoot.rootPath}" addr=${miniRoot.entry.addr}, racing fast vs slow`);

    const fastPromise = this._fastWalk(n, miniRoot, abort.signal).then(result => {
      // If the fast walk returned a partial result, let the slow lookup win instead.
      if (result?.partial) {
        DEBUG && console.debug(`[GritsClient] _lookup_internal → fastWalk partial result for "${n}", deferring to slowLookup`);
        return new Promise(() => {}); // stay pending so slowPromise wins the race
      }
      DEBUG && console.debug(`[GritsClient] _lookup_internal → fastWalk won race for "${n}"`);
      return result;
    }).catch(() => {
      DEBUG && console.debug(`[GritsClient] _lookup_internal → fastWalk rejected for "${n}"`);
      return new Promise(() => {});
    });

    const raceStart = performance.now();
    const result = await Promise.race([fastPromise, slowPromise]);
    const elapsed = performance.now() - raceStart;
    DEBUG && console.debug(`[GritsClient] _lookup_internal → race finished for "${n}" winner=${result?._source ?? 'unknown'} metaHash=${result?.metadataHash} contentHash=${result?.contentHash}`);
    if (result?._source) {
      this._parent._tracker.record(
        result._source === 'fast' ? 'fastWalkHit' : 'slowLookup',
        elapsed
      );
      if (result._source === 'slow') {
        this._parent._tracker.trackContentUrl(n, elapsed);
      }
      delete result._source;
    }
    return result;
  }

  // Lookup that uses a desync override CID as the starting point.
  // First walks as far as possible through local caches (_fastWalk), then asks
  // the server to continue from wherever we got to (if needed).
  async _desyncLookup(targetPath, override) {
    // Exact match — the override addr IS the metadata CID for the target.
    if (override.path === targetPath) {
      if (!override.addr) {
        console.log(`[desync] _desyncLookup("${targetPath}") — exact match, addr is null (unlinked)`);
        return null;
      }
      console.log(`[desync] _desyncLookup("${targetPath}") — exact match, returning override addr directly`);
      return {
        metadataHash: override.addr,
        contentHash:  null,  // resolved by _fetchMeta in lookup()
        contentSize:  0,
      };
    }

    // Ancestor match — walk locally first, then fall back to server for the rest.
    const relativePath = override.path === ''
      ? targetPath
      : targetPath.slice(override.path.length + 1);

    console.log(`[desync] _desyncLookup("${targetPath}") — ancestor override at "${override.path}", local walk for "${relativePath}" from ${override.addr?.slice(0,8)}…`);

    // Use a never-aborting signal since we own this walk (no race with slowLookup).
    const signal = new AbortController().signal;
    const walkResult = await this._fastWalk(
      relativePath,
      { rootPath: '', entry: { addr: override.addr } },
      signal
    ).catch(err => {
      // A structural error (non-dir in path, entry not found) — propagate as not-found.
      console.log(`[desync] _desyncLookup local walk error: ${err.message}`);
      return null;
    });

    if (!walkResult) return null;

    if (!walkResult.partial) {
      // Full cache hit — no server needed.
      console.log(`[desync] _desyncLookup("${targetPath}") — full local hit`);
      return walkResult;
    }

    // Partial walk — ask the server to continue from where we got to.
    const startAddr     = walkResult.metadataHash;
    const serverRelPath = walkResult.remainingPath;
    console.log(`[desync] _desyncLookup("${targetPath}") — partial local walk, server walk for "${serverRelPath}" from ${startAddr?.slice(0,8)}…`);

    const url  = `${this._serverUrl}/grits/v1/lookup`;
    const resp = await fetch(url, {
      method:  'POST',
      headers: this._serverHeaders({ 'Content-Type': 'application/json' }),
      body:    JSON.stringify({
        volume:    this._volume,
        paths:     [serverRelPath],
        startAddr: startAddr,
      }),
    });

    this._updateServiceWorkerHash(resp);

    if (resp.status === 403) {
      const { path: deniedPath } = await resp.json().catch(() => ({ path: targetPath }));
      throw new AccessDeniedError(deniedPath);
    }
    if (!resp.ok) {
      console.log(`[desync] _desyncLookup server walk failed: ${resp.status}`);
      return null;
    }

    const result = await resp.json();
    console.log(`[desync] _desyncLookup server walk returned ${result.paths?.length ?? 0} paths`);

    this._startPrefetch(result.paths ?? []);

    const leaf = result.paths?.find(e => e.path === path);
    if (!leaf || leaf.error) return null;

    return { metadataHash: leaf.addr, contentHash: leaf.contentHash, contentSize: leaf.size ?? 0 };
  }

  // Walk down through cached blobs from miniRoot toward path.
  // Returns one of:
  //   { metadataHash, contentHash, contentSize }            — full hit
  //   { metadataHash, remainingPath, partial: true }        — walked as far as cache allows
  // Throws only on abort or a genuine structural error (e.g. non-dir in path).
  async _fastWalk(path, { rootPath, entry }, signal) {
    // Strip the mini-root prefix to get the remaining path segments to walk.
    const remainder = rootPath === ''
      ? path
      : path.slice(rootPath.length + 1); // skip the trailing '/'
    const parts = remainder ? remainder.split('/') : [];
    DEBUG && console.log(`[fastWalk] walking path="${path}" rootPath="${rootPath}" remainder="${remainder}" parts=[${parts.join(',')}] startAddr=${entry.addr?.slice(0,8)}…`);

    let metaHash = entry.addr;

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      if (signal.aborted) throw new DOMException('aborted', 'AbortError');

      const [meta] = await this._parent._unmarshal(metaHash);
      if (!meta) {
        // Can't read the current node's metadata — return partial from parent.
        const remaining = parts.slice(i).join('/');
        DEBUG && console.log(`[fastWalk] cache miss on meta of "${metaHash.slice(0,8)}…", partial at remaining="${remaining}"`);
        return { metadataHash: metaHash, remainingPath: remaining, partial: true };
      }
      if (meta.type !== 'dir') throw new Error(`fastWalk: expected dir at "${part}", got ${meta.type}`);

      if (signal.aborted) throw new DOMException('aborted', 'AbortError');

      const [dir] = await this._parent._unmarshal(meta.contentHash);
      if (!dir) {
        // Have the dir metadata but not its content listing — return partial from here.
        const remaining = parts.slice(i).join('/');
        DEBUG && console.log(`[fastWalk] cache miss on dir content of "${meta.contentHash.slice(0,8)}…", partial at remaining="${remaining}"`);
        return { metadataHash: metaHash, remainingPath: remaining, partial: true };
      }

      if (!dir[part]) {
        // Entry genuinely not present in this directory listing.
        throw new Error(`fastWalk: "${part}" not found in directory`);
      }

      const childMetaHash = dir[part];
      const [childMeta]   = await this._parent._unmarshal(childMetaHash);
      if (!childMeta) {
        // Have the child's CID but not its metadata yet — return partial pointing at child.
        const remaining = parts.slice(i + 1).join('/');
        DEBUG && console.log(`[fastWalk] cache miss on child meta "${childMetaHash.slice(0,8)}…", partial at remaining="${remaining}"`);
        return { metadataHash: childMetaHash, remainingPath: remaining, partial: true };
      }

      metaHash = childMetaHash;
    }

    if (signal.aborted) throw new DOMException('aborted', 'AbortError');

    // We need the final meta to return contentHash and size.
    const [finalMeta] = await this._parent._unmarshal(metaHash);
    if (!finalMeta) {
      DEBUG && console.log(`[fastWalk] cache miss on final meta "${metaHash.slice(0,8)}…", partial with empty remaining`);
      return { metadataHash: metaHash, remainingPath: '', partial: true };
    }

    return {
      metadataHash: metaHash,
      contentHash:  finalMeta.contentHash,
      contentSize:  finalMeta.size ?? 0,
      _source: 'fast',
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
      method:  'POST',
      headers: this._serverHeaders({ 'Content-Type': 'application/json' }),
      body:    JSON.stringify({ volume: this._volume, paths }),
    });

    this._updateServiceWorkerHash(resp);

    if (DEBUG) {
      // Debug: log raw response before processing.
      const respClone = resp.clone();
      const rawBody = await respClone.text().catch(() => '(unreadable)');
      console.debug(`[GritsClient] _slowLookup path=${path} status=${resp.status} ok=${resp.ok} body=`, rawBody);
    }

    if (resp.status === 403) {
      const { path: deniedPath } = await resp.json().catch(() => ({ path }));
      DEBUG && console.debug(`[GritsClient] _slowLookup → 403 access_denied for ${deniedPath}`);
      throw new AccessDeniedError(deniedPath);
    }
    if (!resp.ok) {
      DEBUG && console.debug(`[GritsClient] _slowLookup → !ok (status=${resp.status}), returning null`);
      return null;
    }

    const result = await resp.json();
    this._ingestLookupResponse(result);

    if (!this._configFetched) this._fetchVolumeConfig();

    // Find the entry for the path we actually asked for. The response may
    // contain additional entries for mini-root refresh — the last entry is
    // not necessarily ours.
    if (!result.paths || result.paths.length <= 0) {
      DEBUG && console.debug(`[GritsClient] _slowLookup → no paths in result, throwing`);
      throw new Error(`lookup: No paths returned looking up ${path}`);
    }

    const entry = result.paths.find(p => p.path === path);
    DEBUG && console.debug(`[GritsClient] _slowLookup paths=${result.paths.map(p => `${p.path}(err=${p.error})`).join(', ')} entry=${entry?.path} entry.error=${entry?.error}`);

    if (!entry) {
      DEBUG && console.debug(`[GritsClient] _slowLookup → path "${path}" not in response, returning null`);
      return null;
    }

    if (entry.error === 'not_found') { DEBUG && console.debug(`[GritsClient] _slowLookup → not_found, returning null`); return null; }
    if (entry.error === 'access_denied') { DEBUG && console.debug(`[GritsClient] _slowLookup → access_denied, throwing`); throw new AccessDeniedError(entry.path ?? path); }
    if (entry.error) { DEBUG && console.debug(`[GritsClient] _slowLookup → unexpected error ${entry.error}, throwing`); throw new Error(`lookup: unexpected error at ${entry.path}: ${entry.error}`); }

    DEBUG && console.debug(`[GritsClient] _slowLookup → success for "${path}" addr=${entry.addr} contentHash=${entry.contentHash} size=${entry.size ?? 0}`);
    return { metadataHash: entry.addr, contentHash: entry.contentHash, contentSize: entry.size ?? 0, _source: 'slow' };
  }

  // Ingest a lookup/link response: cache all returned blobs and update
  // any mini-root entries whose paths appear in the response.
  _ingestLookupResponse(result) {
    if (!result?.paths?.length) return;

    // Rebuild mini-roots from scratch: keep only successful entries that have
    // no successful ancestor also present in this response.
    const successPaths = result.paths.filter(e => !e.error && e.addr);
    this._miniRoots = new Map();
    for (const entry of successPaths) {
      const hasSuccessfulAncestor = successPaths.some(
        other => other.path !== entry.path && _isAncestorOrSelf(other.path, entry.path)
      );
      if (!hasSuccessfulAncestor) {
        this._miniRoots.set(entry.path, { addr: entry.addr, ts: Date.now() });
        DEBUG && console.log(`[miniRoot] upsert "${entry.path}" → ${entry.addr?.slice(0,8)}…`);
      }
    }

    DEBUG && console.log(`[miniRoot] full map after rebuild:`, JSON.stringify([...this._miniRoots.entries()]));

    this._startPrefetch(successPaths);
  }

  // ── Internal: prefetch ────────────────────────────────────────

  _startPrefetch(paths) {
    if (this._parent._swControlled) return;

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
// Assertion checking (client-side, for desync mode)
//
// Returns an error string if the assertion fails, or null if it passes.
// `currentFile` is the GritsFile at `path` right now, or null if absent.
// ─────────────────────────────────────────────────────────────────

function _checkAssertions(assert, prevAddr, currentFile) {
  if (assert & ASSERT_PREV_MATCHES) {
    const actualAddr = currentFile ? currentFile.cid() : '';
    if (actualAddr !== prevAddr)
      return `ASSERT_PREV_MATCHES failed: expected prev=${prevAddr}, got ${actualAddr}`;
  }
  if (assert & ASSERT_IS_BLOB) {
    if (!currentFile || !currentFile.isFile())
      return `ASSERT_IS_BLOB failed: path is not a blob`;
  }
  if (assert & ASSERT_IS_TREE) {
    if (!currentFile || !currentFile.isDir())
      return `ASSERT_IS_TREE failed: path is not a directory`;
  }
  if (assert & ASSERT_IS_NONEMPTY) {
    if (!currentFile || currentFile.size() === 0)
      return `ASSERT_IS_NONEMPTY failed: path is absent or empty`;
  }
  return null;
}

// ─────────────────────────────────────────────────────────────────
// GritsClient — cache operations only, no server contact
// ─────────────────────────────────────────────────────────────────

class GritsClient {
  constructor() {
    this._swControlled = false;
    this._swReloadTriggered = false;   // one-shot guard for navigate-reload on stale SW
    this._swRegistrationAttempted  = false;

    this._local     = new Map(); // cid → Uint8Array  (synthesized, pending upload)
    this._jsonCache = new Map(); // cid → { data, lastAccessed }
    this._blobCache = null;      // browser Cache API
    this._volumes   = new Map(); // volKey → GritsVolume

    // SW hash seen per server URL.
    //   undefined — no contact with that server yet
    //   null      — server responded but header was absent (SW module not installed)
    //   string    — hash value
    this._serviceWorkerHashes = new Map(); // serverUrl → string | null | undefined

    // Extra headers merged into every outgoing server request.
    // Used by the SW test harness to forward hash override headers onto
    // lookup/link/upload requests without modifying GritsClient internals.
    this.extraHeaders = {};

    // Bootstrap auth token from sessionStorage (set by login.html).
    // Guarded for environments without sessionStorage (e.g. service workers).
    let savedToken = null;
    try { savedToken = sessionStorage.getItem('grits-auth-token'); } catch { /* no sessionStorage */ }
    if (savedToken) {
      this._authToken = savedToken;
      this.extraHeaders['X-Grits-Auth-Token'] = savedToken;
      try {
        const raw = atob(savedToken);
        this._username = raw.split(':')[1] || '';
      } catch { this._username = ''; }
    } else {
      this._authToken = null;
      this._username  = '';
    }

    this._verifier = new HashVerifier({ debug: DEBUG });
    this._tracker  = new PerformanceTracker({
      enabled:      DEBUG_STATS,
      interval:     10_000,
      mirrorStatsFn: () => this._collectMirrorStats(),
    });

    this._initBlobCache();
    this._cleanupTimer = setInterval(() => this._cleanupJsonCache(), JSON_CACHE_CLEANUP_INTERVAL);
  }

  // ── Auth ──────────────────────────────────────────────────
  // Session token management. The token is an HMAC-signed value
  // returned by the server on login. It's stored in memory only
  // and sent as X-Grits-Auth-Token on every request.

  /** Log in to the given server URL. Stores the token and username. */
  async login(serverUrl, username, password) {
    const res = await fetch(`${serverUrl}/grits/v1/auth/login`, {
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      body:    JSON.stringify({ username, password }),
    });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || 'login failed');
    }
    const { token } = await res.json();
    if (!token) throw new Error('login: no token in response');
    this._authToken = token;
    this._username  = username;
    this.extraHeaders['X-Grits-Auth-Token'] = token;
  }

  /** Log out — clears the stored token. */
  logout() {
    delete this.extraHeaders['X-Grits-Auth-Token'];
    this._authToken = null;
    this._username  = '';
    sessionStorage.removeItem('grits-auth-token');
  }

  /** Returns the current username, or empty string if not logged in. */
  whoami() {
    return this._username ?? '';
  }

  destroy() {
    clearInterval(this._cleanupTimer);
    this._tracker.destroy();
  }

  /** Access the performance tracker (for custom recording or snapshots). */
  get tracker() { return this._tracker; }

  _flushCaches() {
    this._local.clear();
    this._jsonCache.clear();
    if (this._blobCache) this._blobCache.keys().then(keys =>
      keys.forEach(key => this._blobCache.delete(key)));
  }

  // ── Volume registration ───────────────────────────────────────

  volume(serverUrl, volumeName) {
    if (typeof serverUrl  !== 'string') throw new TypeError(`volume: serverUrl must be a string, got ${_typename(serverUrl)}`);
    if (typeof volumeName !== 'string') throw new TypeError(`volume: volumeName must be a string, got ${_typename(volumeName)}`);
    const key = _volKey(serverUrl, volumeName);
    if (!this._volumes.has(key))
      this._volumes.set(key, new GritsVolume(serverUrl, volumeName, this));
    return this._volumes.get(key);
  }

  // Derive { serverUrl, volume, path } from a module URL
  fromModule(moduleUrl) {
    if (typeof moduleUrl !== 'string') throw new TypeError('fromModule: moduleUrl must be a string');
    const u = new URL(moduleUrl);
    const parts = u.pathname.split('/').filter(Boolean);

    // Expect: grits/v1/content/{volume}/{path...}
    if (parts.length < 4 || parts[0] !== 'grits' || parts[2] !== 'content') {
      throw new Error(`fromModule: unsupported URL format: ${moduleUrl}`);
    }

    const volume = parts[3];
    const path   = parts.slice(4).join('/');

    return {
      serverUrl: u.origin,
      volume,
      path,
    };
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
    // 1. Hot JSON cache — fastest path.
    const cached = this._jsonCache.get(hash);
    if (cached) {
      cached.lastAccessed = Date.now();
      DEBUG && console.log(`[_unmarshal] ${hash} source=jsonCache`);
      return [cached.data, 'memory'];
    }

    // 2. Local synthesized blobs (mkfile/mkdir output not yet uploaded).
    const local = this._local.get(hash);
    if (local) {
      try {
        const data = JSON.parse(new TextDecoder().decode(local));
        this._jsonCache.set(hash, { data: _deepFreeze(data), lastAccessed: Date.now() });
        DEBUG && console.log(`[_unmarshal] ${hash} source=local`);
        return [data, 'local'];
      } catch (_) {}
    }

    // 3. Browser blob cache — no network I/O, just IndexedDB.
    const blobResp = await this._blobCacheGet(hash);
    if (blobResp) {
      try {
        const data = await blobResp.json();
        this._jsonCache.set(hash, { data: _deepFreeze(data), lastAccessed: Date.now() });
        DEBUG && console.log(`[_unmarshal] ${hash} source=blobCache`);
        return [data, 'blobCache'];
      } catch (_) {}
    }

    DEBUG && console.log(`[_unmarshal] ${hash} source=miss`);
    return [null, null];
  }

  _cleanupJsonCache() {
    const cutoff = Date.now() - JSON_CACHE_MAX_AGE;
    for (const [k, v] of this._jsonCache)
      if (v.lastAccessed < cutoff) this._jsonCache.delete(k);
  }

  // ── Internal: Browser blob cache (used by GritsVolume) ───────

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

  // ── Internal: Misc ────────────────────────────────────────────

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

function _updateSwMode(swControlled) {
  if (SW_CONTROLLED === swControlled) return;
  SW_CONTROLLED = swControlled;
  console.log(`[GritsClient] SW_CONTROLLED → ${swControlled}`);
}

// ── Exports ───────────────────────────────────────────────────────

export { ASSERT_PREV_MATCHES, ASSERT_IS_BLOB, ASSERT_IS_TREE, ASSERT_IS_NONEMPTY }; // %FOR MODULE%
export { AssertionError, AccessDeniedError, GritsFile, GritsVolume };  // %FOR MODULE%
export default GritsClient;                                            // %FOR MODULE%
