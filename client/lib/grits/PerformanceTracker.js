// PerformanceTracker.js — Observability for Grits operations
//
// Collects timing and count stats across cache hits, misses, lookups,
// prefetches, and mirror performance.  Periodically logs a summary.
//
// Usage:
//   const tracker = new PerformanceTracker({ interval: 10_000 });
//   tracker.record('blobCacheHit', elapsed);
//   tracker.count('prefetchSuccess');
//   tracker.trackContentUrl('/foo/bar.js', elapsed);
//   tracker.destroy();

const DEFAULT_LOG_INTERVAL = 10_000; // ms

export default class PerformanceTracker {
  /**
   * @param {Object}  opts
   * @param {number}  [opts.interval=10000]  - Logging interval in ms (0 = disabled)
   * @param {boolean} [opts.enabled=true]    - Master switch
   * @param {Function} [opts.mirrorStatsFn]  - Optional callback returning mirror stats array
   */
  constructor({ interval = DEFAULT_LOG_INTERVAL, enabled = true, mirrorStatsFn = null } = {}) {
    this._enabled       = enabled;
    this._mirrorStatsFn = mirrorStatsFn;
    this._timer         = null;

    this.reset();

    if (enabled && interval > 0) {
      this._timer = setInterval(() => this.log(), interval);
    }
  }

  destroy() {
    if (this._timer) { clearInterval(this._timer); this._timer = null; }
  }

  // ── Recording API ─────────────────────────────────────────────

  /**
   * Record a timing sample for a named bucket.
   * Also increments the count for that bucket.
   */
  record(bucket, elapsedMs) {
    if (!this._enabled) return;
    this._ensure(bucket);
    this._buckets[bucket].timings.push(elapsedMs);
    this._buckets[bucket].count++;
  }

  /** Increment a counter without a timing sample. */
  count(bucket, n = 1) {
    if (!this._enabled) return;
    this._ensure(bucket);
    this._buckets[bucket].count += n;
  }

  /** Track a slow-path content URL fetch. */
  trackContentUrl(url, elapsedMs) {
    if (!this._enabled) return;
    this._contentUrls.push({ url, latency: elapsedMs.toFixed(2) });
  }

  // ── Querying API ──────────────────────────────────────────────

  /** Return current snapshot (without resetting). */
  snapshot() {
    const out = {};
    for (const [name, b] of Object.entries(this._buckets)) {
      out[name] = { count: b.count, avgMs: _avg(b.timings) };
    }
    out._contentUrls = [...this._contentUrls];
    return out;
  }

  // ── Logging ───────────────────────────────────────────────────

  log() {
    if (!this._enabled) return;

    const hasBucketActivity = Object.values(this._buckets).some(b => b.count > 0);
    const hasContentUrls    = this._contentUrls.length > 0;

    if (hasBucketActivity) {
      const parts = [];
      for (const [name, b] of Object.entries(this._buckets)) {
        if (b.count === 0) continue;
        const avg = b.timings.length ? ` (avg ${_avg(b.timings)}ms)` : '';
        parts.push(`${name}: ${b.count}${avg}`);
      }
      console.log(
        `%c[GRITS STATS]%c ${parts.join(' | ')}`,
        'color: #22c55e; font-weight: bold', 'color: inherit'
      );
    }

    if (hasContentUrls) {
      console.log(
        `%c[CONTENT URLS]%c Direct fetches:`,
        'color: #f59e0b; font-weight: bold', 'color: inherit'
      );
      for (const item of this._contentUrls) {
        console.log(`  ${item.url}: ${item.latency}ms`);
      }
    }

    // Mirror stats (if provider given)
    if (this._mirrorStatsFn) {
      try {
        const mirrorStats = this._mirrorStatsFn();
        if (mirrorStats?.length) {
          let loggedHeader = false;
          for (const stat of mirrorStats) {
            if ((stat.rawBytes ?? 0) === 0) continue;
            if (!loggedHeader) {
              console.log(
                `%c[MIRROR STATS]%c (${mirrorStats.length} mirrors)`,
                'color: #3b82f6; font-weight: bold', 'color: inherit'
              );
              loggedHeader = true;
            }
            const host = new URL(stat.url).hostname;
            console.log(
              `  ${host}: Latency ${stat.latency} | Bandwidth ${stat.bandwidth} | ` +
              `Reliability ${stat.reliability} | Data ${stat.bytesFetched}`
            );
          }
        }
      } catch (_) { /* mirror stats unavailable */ }
    }

    this.reset();
  }

  // ── Internal ──────────────────────────────────────────────────

  reset() {
    this._buckets     = {};
    this._contentUrls = [];
  }

  _ensure(name) {
    if (!this._buckets[name]) {
      this._buckets[name] = { count: 0, timings: [] };
    }
  }
}

function _avg(arr) {
  if (!arr.length) return 'N/A';
  return (arr.reduce((s, v) => s + v, 0) / arr.length).toFixed(2);
}
