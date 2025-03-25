/**
 * GritsClient - Client-side interface for interacting with the Grits file storage API
 */

const debugClientTiming = false;
const debugPerformanceStats = true;

const CLEANUP_INTERVAL = 5 * 60 * 1000; // ms

class GritsClient {

  /////
  // Top level

  constructor(config) {
    // Basic configuration
    this.serverUrl = config.serverUrl.replace(/\/$/, '');
    this.volume = config.volume;
  
    // Cache settings
    this.cacheMaxAge = 5 * 60 * 1000; // 5 minutes for less-used items
    this.softTimeout = config.softTimeout || 30 * 1000; // 30 seconds default
    this.hardTimeout = config.hardTimeout || 5 * 60 * 1000; // 5 minutes default
    
    // Caching structures
    this.rootHash = null;
    this.rootHashTimestamp = 0; // When we last received the root hash
    this.jsonCache = new Map(); // Key: hash, Value: {data: Object, lastAccessed: timestamp}
    
    // Initialize mirror manager
    this.mirrorManager = new MirrorManager({
      serverUrl: this.serverUrl,
      volume: this.volume,
      debug: debugClientTiming,
      refreshInterval: config.mirrorRefreshInterval,
      mirrorStatsInterval: config.mirrorStatsInterval
    });
      
    // Start the mirror manager
    this.mirrorManager.initialize().catch(err => {
      console.error("Error initializing mirror manager:", err);
    });
      
    // TODO: In-flight lookups for early returns

    // Prefetch data
    this._inFlightPrefetches = new Map();
    this._prefetchQueue = [];
    this._isProcessingQueue = false;

    // Setup periodic cleanup
    this.cleanupInterval = setInterval(() => this._cleanupCaches(), CLEANUP_INTERVAL);

    // Setup stats interval
    this.resetStats();
  
    // Reset stats periodically and log them
    if (debugPerformanceStats) {
      this.statsInterval = setInterval(() => this._logStats(), 10000);
    }

    // Store service worker hash, for detection and reload when needed
    this.serviceWorkerHash = undefined;

    // Initialize blob cache
    this._initializeBlobCache().catch(err => {
      console.error("Error during blob cache initialization:", err);
    });

    this.debugLog("constructor", `Initialized with soft timeout: ${this.softTimeout}ms, hard timeout: ${this.hardTimeout}ms`);
  }

  destroy() {
    clearInterval(this.cleanupInterval);
    if (this.statsInterval) {
      clearInterval(this.statsInterval);
    }
  }
  
  resetRoot() {
    this.rootHashTimestamp = 0;
    this.debugLog("resetRoot", "Root hash timestamp reset to 0");
  }
  
  /////
  // Fetching and merkle tree lookup logic

  extractExtension(path) {
    // Extract extension from path
    const pathParts = path.split('/');
    const filename = pathParts[pathParts.length - 1]; // Get just the filename

    // Only look for extension in the filename part
    const lastDotIndex = filename.lastIndexOf('.');
    const extension = (lastDotIndex > 0) ? filename.substring(lastDotIndex + 1) : null;

    return extension;
  }

  async fetchFile(path) {
    this.debugLog(path, `fetchFile(${path})`);

    const startTime = performance.now();
    this.stats.totalRequests++;

    const normalizedPath = this._normalizePath(path);
    
    // Check root hash age to determine strategy
    const now = Date.now();
    const rootAge = now - this.rootHashTimestamp;
    
    let result;
    
    // We can fetch stuff quick, if we have it in cache
    this.debugLog(path, "  try fast path");
    const pathInfo = await this._tryFastFetch(normalizedPath);
    if (pathInfo && pathInfo.contentHash) {
        this.debugLog(path, "  succeed (metadata at least)");

        const extension = this.extractExtension(path);

        // Find the blob (hopefully it's in the cache, but if not is fine)
        const response = await this._innerFetch(pathInfo.contentHash, startTime, extension);
        
        // Return response directly without wrapping
        result = response;
    } else {
        // Content lookup path
        this.stats.contentLookups++;
        result = await this._slowFetch(normalizedPath, startTime);
        this.stats.timings.contentLookups.push(performance.now() - startTime);
    }

    this.debugLog(path, "  all done");

    return result;
  }
  
  async _tryFastFetch(path) {
    const startTime = performance.now();
    let memoryHits = 0;
    let browserCacheHits = 0;
    let networkHits = 0;
    
    // Can't do anything without a root hash
    if (!this.rootHash) {
      this.debugLog(path, "null root");
      return null;
    }
    
    // Shouldn't do anything if we're past the hard timeout
    if (Date.now() - this.rootHashTimestamp > this.hardTimeout) {
      this.debugLog(path, "Past hard timeout");
      return null;
    }

    const components = path.split('/').filter(c => c.length > 0);
    let currentMetadataHash = this.rootHash;
    let currentContentHash = null;
    let currentContentSize = null;
    
    let triedIndexHtml = false;
  
    this.debugLog(path, `Finding fast path from ${this.rootHash}`);
    
    // Helper function to track fetch sources
    const trackJsonSource = (source) => {
      if (source === 'memory') memoryHits++;
      else if (source === 'browserCache') browserCacheHits++;
      else if (source === 'network') networkHits++;
    };
    
    // Start with the root, then follow each path component
    for (let i = 0; i < components.length; i++) {
      const component = components[i];
      
      // First, get the parent metadata
      const [metadata, metaSource] = await this._fetchAndUnmarshal(currentMetadataHash, false);
      trackJsonSource(metaSource);
      
      if (!metadata || metadata.type !== 'dir') {
        this.debugLog(path, `  fail: ${component}: parent not a dir`);
        return null; // Not a directory, can't continue
      }
      
      // Get the directory listing
      const [directoryListing, dirSource] = await this._fetchAndUnmarshal(metadata.contentHash, false);
      trackJsonSource(dirSource);
      
      if (!directoryListing || !directoryListing[component]) {
        this.debugLog(path, `  fail: ${component}: not found in ${metadata.contentHash}`);
        this.debugLog(path, `    ${JSON.stringify(directoryListing)}`);
        return null; // Component not found in directory
      }
      
      // Get child's metadata
      const childMetadataHash = directoryListing[component];
      const [childMetadata, childSource] = await this._fetchAndUnmarshal(childMetadataHash, false);
      trackJsonSource(childSource);
      
      if (!childMetadata) {
        this.debugLog(path, `  fail: ${component}: no JSON for metadata`);
        return null;
      }
      
      // Continue with this child as the new current node
      currentMetadataHash = childMetadataHash;
      currentContentHash = childMetadata.contentHash;
      currentContentSize = childMetadata.size || 0;
  
      // Okay, this is a little weird... we check if we're about to return a bare directory, and if
      // so, we back up and do one more step with index.html and some hackery.
      if (i == components.length-1 && !triedIndexHtml) {
        triedIndexHtml = true;
        const [currentMetadata, indexHtmlSource] = await this._fetchAndUnmarshal(currentMetadataHash);
        trackJsonSource(indexHtmlSource);
        
        this.debugLog(path, `  double-check for index.html: ${JSON.stringify(currentMetadata, null, 2)}`)
        if (currentMetadata.type != 'blob') {
          this.debugLog(path, '    found!');
          components[i] = 'index.html';
          i--;
        }
      }
    }
    
    const lookupTime = performance.now() - startTime;
    this.debugLog(path, `Complete fast path lookup! hash is ${currentContentHash}`);
  
    // If we got here, we resolved the entire path
    return {
      metadataHash: currentMetadataHash,
      contentHash: currentContentHash,
      contentSize: currentContentSize
    };
  }

  async _slowFetch(path, startTime) {
    this.debugLog(path, "Slow fetching via HEAD request to /grits/v1/content");
    
    const normalizedPath = this._normalizePath(path);
    const url = `${this.serverUrl}/grits/v1/content/${this.volume}/${normalizedPath}`;
    
    try {
      // First make a HEAD request to get metadata without the content
      const headResponse = await fetch(url, { 
        method: 'HEAD',
        headers: {
          'Cache-Control': 'no-cache'
        }
      });
      
      // Check if we got a valid response
      if (!headResponse.ok) {
        throw new Error(`HEAD request failed with status ${headResponse.status}`);
      }
      
      // Update service worker hash if available
      const newClientHash = headResponse.headers.get('X-Grits-Service-Worker-Hash');
      if (newClientHash) {
        this.serviceWorkerHash = newClientHash;
      }
      
      const metadataJson = headResponse.headers.get('X-Path-Metadata-JSON');
      if (!metadataJson) {
        this.debugLog("headers", "No X-Path-Metadata-JSON header found");
        return;
      }
      
      // Parse the JSON data
      const pathMetadata = JSON.parse(metadataJson);
      this.debugLog("headers", `Parsed ${pathMetadata.length} path metadata entries`);
  
      // Process each entry
      for (const entry of pathMetadata) {
        // With the new format, the root entry still needs to be identified
        // but the property names have changed
        if (entry.path === "") {
          this.debugLog("rootHash", `Updating root hash: ${entry.metadataHash}`);
          this.rootHash = entry.metadataHash;
          this.rootHashTimestamp = Date.now();
        }
      }

      // Start background prefetching
      this.debugLog(path, `Starting prefetch: ${JSON.stringify(pathMetadata, null, 2)}`);
      this._startMetadataPrefetch(pathMetadata);

      // Extract extension from path
      const extension = this.extractExtension(path);
      
      // Get the content hash from the last entry's contentHash property
      const lastEntry = pathMetadata[pathMetadata.length-1];
      const contentHash = lastEntry.contentHash;
      this.debugLog(path, `  content hash is ${contentHash}`);

      const latency = performance.now() - startTime;
      this.stats.contentUrls.push({
        url: normalizedPath,
        latency: latency.toFixed(2)
      });

      // Pass the extension to _innerFetch
      const contentResponse = await this._innerFetch(contentHash, startTime, extension);
      
      return contentResponse;
    } catch (error) {
      console.error(`Error during metadata fetch for ${path}:`, error);
      throw error;
    }
  }

  /////
  // Prefetching for various JSON bits

  // TODO - have a max size for the queue, and just throw stuff away if it has too many entries
  // already
  _startMetadataPrefetch(pathMetadata) {
    // Add metadata entries to prefetch queue
    for (const entry of pathMetadata) {
      const hash = entry.metadataHash;
      if (!this._inFlightPrefetches.has(hash)) {
        // Note - we don't check for jsonCache() just in case by some weird chance, the metadata
        // is somehow in jsonCache but not the contents of the dir. If we skip it in that case,
        // there's no way it can ever make its way back in.

        this._prefetchQueue.push(hash);
        // Mark this hash as in-flight so we don't queue it again
        this._inFlightPrefetches.set(hash, true);
      }
    }
    
    if (!this._isProcessingQueue) {
      this._isProcessingQueue = true;    
      this._processNextInPrefetchQueue();
    }
  }

  async _processNextInPrefetchQueue() {
    while (this._prefetchQueue.length > 0) {
      const hash = this._prefetchQueue.shift();
      
      try {
        this.debugLog("prefetch", `Fetching hash ${hash}`);

        const startTime = performance.now();
        
        let jsonData;
        // Check again if it's already in cache (might have been added since queuing)
        if (!this.jsonCache.has(hash)) {
          this.debugLog("prefetch:" + hash.substring(0, 8), "fetch");

          const response = await this._innerFetch(hash, null);
          
          this.debugLog("prefetch:" + hash.substring(0, 8), `response: ${response.status}`);

          if (!response.ok) {
            this.debugLog("prefetch:" + hash.substring(0, 8), `fail`);
            continue;
          }

          jsonData = await response.json();
          this.jsonCache.set(hash, {
            data: jsonData,
            lastAccessed: Date.now()
          });
          this.stats.prefetchSuccesses++;
          this.debugLog("prefetch:" + hash.substring(0, 8), `inserted ${JSON.stringify(jsonData, null, 2)} at ${hash}`);
        } else {
          jsonData = this.jsonCache.get(hash).data;
        }

        this.debugLog("prefetch:" + hash.substring(0, 8), "metadata done");

        if (jsonData.type == 'dir' && !this.jsonCache.has(jsonData.contentHash)) {
          // Do it all over again for the content

          const contentHash = jsonData.contentHash;

          this.debugLog("prefetch:" + contentHash.substring(0, 8), "content fetch");

          const response = await this._innerFetch(contentHash, null);
          this.debugLog("prefetch:" + contentHash.substring(0, 8), `response: ${response.status}`);

          if (!response.ok) {
            this.debugLog("prefetch:" + contentHash.substring(0, 8), `fail`);
            continue;
          }

          jsonData = await response.json();
          this.jsonCache.set(contentHash, {
            data: jsonData,
            lastAccessed: Date.now()
          });
          this.stats.prefetchSuccesses++;
          this.debugLog("prefetch:" + hash.substring(0, 8), `inserted ${JSON.stringify(jsonData, null, 2)} at ${contentHash}`);

          this.debugLog("prefetch:" + hash.substring(0, 8), "content done");
        }
      } catch (error) {
        console.error(`Error prefetching ${hash}: ${error.message}`);
      } finally {
        // Remove from in-flight tracking regardless of success/failure
        this._inFlightPrefetches.delete(hash);
      }
      
      // Small delay to avoid overwhelming the browser
      await new Promise(resolve => setTimeout(resolve, 10));
    }
    
    this._isProcessingQueue = false;
  }

  async _fetchAndUnmarshal(hash, forceFetch = false) {
    const startTime = performance.now();
    
    // Check if we have it in our memory cache
    if (this.jsonCache.has(hash)) {
      const entry = this.jsonCache.get(hash);
      // Update last accessed time
      entry.lastAccessed = Date.now();
      this.debugLog("hash:" + hash.substring(0, 8), `Memory cache hit (${Math.round(performance.now() - startTime)}ms)`);
      return [entry.data, 'memory'];
    }
    
    if (!forceFetch) {
      // If it's not in the cache, we fail immediately.
      return [null, null];
    }

    this.debugLog("hash:" + hash.substring(0, 8), 'network fetch');

    const response = await this._innerFetch(hash, startTime);
    
    this.debugLog("hash:" + hash.substring(0, 8), 'network fetch done');

    const fetchTime = Math.round(performance.now() - startTime);
    this.debugLog("hash:" + hash.substring(0, 8), `Network fetch completed in ${fetchTime}ms, status ${response.status}`);
    
    if (!response.ok) {
      console.error(`Failed to get JSON for ${hash}:`, response.status);
      return [null, null];
    }
  
    const jsonStart = performance.now();
    this.debugLog("hash:" + hash.substring(0, 8), 'JSON parse');
    const data = await response.json();
    this.debugLog("hash:" + hash.substring(0, 8), 'JSON parse done');
    const jsonTime = Math.round(performance.now() - jsonStart);
    this.debugLog("hash:" + hash.substring(0, 8), `JSON parsing took ${jsonTime}ms`);

    // Cache it with current timestamp
    this.jsonCache.set(hash, {
      data,
      lastAccessed: Date.now()
    });
    
    return [data, 'network'];
  }

  /////
  // Blob cache

  async _initializeBlobCache() {
    try {
      // Open a dedicated cache for blobs
      this.blobCache = await caches.open(`grits-blobs-${this.volume}`);
      this.debugLog("cache", "Blob cache initialized");
    } catch (error) {
      console.error("Failed to initialize blob cache:", error);
      // Fallback - we'll use network requests directly
      this.blobCache = null;
    }
  }

  async _innerFetch(hash, startTime = null, extension = null) {
    // Check if caching is available
    if (this.blobCache) {
        // Try to get from cache first
        const cachedResponse = await this.blobCache.match(hash);
        if (cachedResponse) {
            if (startTime) {
                this.stats.blobCacheHits++;
                this.stats.timings.blobCacheHits.push(performance.now() - startTime);
            }
            this.debugLog("cache:" + hash.substring(0, 8), "Blob cache hit");
            return cachedResponse;
        }
    }
    
    // If not in cache or caching unavailable, fetch from network
    // We use mirror manager unconditionally; it'll fall back to the origin server if needed.
    this.debugLog("cache:" + hash.substring(0, 8), "Blob cache miss, fetching from network");

    let response;
    if (true) {
      // Option 1:
      const rightBeforeTime = performance.now();
      response = await this.mirrorManager.fetchBlob(hash, extension);
      const afterFetchTime = performance.now();
      
      // Calculate and log the timing information
      const totalElapsed = startTime ? (afterFetchTime - startTime).toFixed(2) : 'N/A';
      const fetchElapsed = (afterFetchTime - rightBeforeTime).toFixed(2);
      
      console.log(`Fetch timing for ${hash.substring(0, 8)}: 
        - Total elapsed: ${totalElapsed} ms
        - Mirror fetch elapsed: ${fetchElapsed} ms`);    
    } else {
      // Option 2:
      let url = `${this.serverUrl}/grits/v1/blob/${hash}`;
      if (extension) {
          url += `.${extension}`;
      }
      response = await fetch(url);
    }

    if (startTime) {
        this.stats.timings.blobCacheMisses.push(performance.now() - startTime);
        this.stats.blobCacheMisses++;
    }
            
    // Store a clone in the cache for future use (if caching is available)
    if (this.blobCache && response.ok) {
        // We need to clone the response because it can only be consumed once
        const clonedResponse = response.clone();
        this.blobCache.put(hash, clonedResponse).catch(err => {
            console.error(`Failed to cache blob ${hash}:`, err);
        });
    }
    
    return response;
  }

  /////
  // Performance tracking

  _logStats() {
    // Calculate averages for regular stats
    const calcAvg = arr => arr.length ? 
      (arr.reduce((sum, val) => sum + val, 0) / arr.length).toFixed(2) : 
      'N/A';
    
    const contentAvg = (this.stats.timings.contentLookups.length ? ` (avg ${calcAvg(this.stats.timings.contentLookups)}ms)` : '');
    const hitAvg = (this.stats.timings.blobCacheHits.length ? ` (avg ${calcAvg(this.stats.timings.blobCacheHits)}ms)` : '');
    const missAvg = (this.stats.timings.blobCacheMisses.length ? ` (avg ${calcAvg(this.stats.timings.blobCacheMisses)}ms)` : '');
    
    // Log regular stats
    if (this.stats.totalRequests > 0) {
      console.log(
        `%c[GRITS STATS]%c Last 10s: ` + 
        `Requests: ${this.stats.totalRequests} | ` +
        `Content lookups: ${this.stats.contentLookups}${contentAvg} | ` + 
        `Blob cache hits: ${this.stats.blobCacheHits}${hitAvg} | ` +
        `Blob cache misses: ${this.stats.blobCacheMisses}${missAvg} | ` +
        `Prefetch successes: ${this.stats.prefetchSuccesses}`,
        'color: #22c55e; font-weight: bold', 'color: inherit'
      );
    }
    
    if (this.stats.contentUrls.length > 0) {
      console.log(
        `%c[CONTENT URLS]%c Direct fetches from /grits/v1/content:`,
        'color: #f59e0b; font-weight: bold', 'color: inherit'
      );
      
      // Log each URL with its latency
      this.stats.contentUrls.forEach(item => {
        console.log(`  ${item.url}: ${item.latency}ms`);
      });
    }

    // Now log mirror stats
    const mirrorStats = this.mirrorManager.getMirrorStats();
    if (mirrorStats.length > 0) {
      let loggedAny = false;
      // Log one line per mirror
      for (const stat of mirrorStats) {
        if (stat.bytesFetched > 0) {
          if (!loggedAny) {
            console.log(`%c[MIRROR STATS]%c (${mirrorStats.length} mirrors)`, 
              'color: #3b82f6; font-weight: bold', 'color: inherit');
            loggedAny = true;
          }
              
          const host = new URL(stat.url).hostname;
          console.log(
            `  ${host}: Latency ${stat.latency} | Bandwidth ${stat.bandwidth} | ` +
            `Reliability ${stat.reliability} | Requests ${stat.requests} | ` +
            `Data ${stat.bytesFetched}`
          );
          loggedAny = true;
        }
      }
    }
    
    // Reset stats for next interval
    this.resetStats();
    this.mirrorManager.resetStats();
  }

  /////
  // Misc

  resetStats() {
    this.stats = {
      // Reset regular stats
      totalRequests: 0,
      contentLookups: 0,
      blobCacheHits: 0,
      blobCacheMisses: 0,
      prefetchSuccesses: 0,
      timings: {
        contentLookups: [],
        blobCacheHits: [],
        blobCacheMisses: []
      },
      contentUrls: [] // Reset the content URLs array
    };
  }

  getServiceWorkerHash() {
    return this.serviceWorkerHash;
  }

  _cleanupCaches() {
    const now = Date.now();
    const expiredTime = now - this.cacheMaxAge;
    
    // Clean JSON cache based on last access time
    for (const [hash, entry] of this.jsonCache.entries()) {
      if (entry.lastAccessed < expiredTime) {
        this.jsonCache.delete(hash);
      }
    }
  }

  _normalizePath(path) {
    return path.replace(/^\/+|\/+$/g, '');
  }

  /**
   * Debug logger function that prints timestamps with path information
   * @param {string} path - The full path to log (last component will be removed)
   * @param {string} message - The debug message to display
   * @param {Object} [options] - Optional configuration
   * @param {boolean} [options.includeDate=false] - Include date in timestamp
   * @param {boolean} [options.color=true] - Use colored console output
   * @param {string} [options.prefix='DEBUG'] - Prefix for the log message
   */
  debugLog(path, message, options = {}) {
    if (!debugClientTiming) {
      return;
    }

    // Default options
    const config = {
      includeDate: false,
      color: true,
      prefix: 'DEBUG',
      ...options
    };
    
    // Get current time
    const now = new Date();
    
    // Format time string (minute:second.millisecond)
    const timeString = config.includeDate
      ? now.toISOString()
      : `${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}.${String(now.getMilliseconds()).padStart(3, '0')}`;
    
    // Remove last component from path
    const pathParts = path.split('/');
    const file = pathParts.pop();
    
    // Prepare the log message
    const logPrefix = `[${config.prefix}][${timeString}][${file}]`;
    
    // Use colors if enabled and running in an environment that supports them
    if (config.color && typeof window === 'undefined') {
      // Node.js-like environment
      console.log(`\x1b[36m${logPrefix}\x1b[0m ${message}`);
    } else if (config.color && typeof window !== 'undefined' && window.console && window.console.log) {
      // Browser environment with color support
      console.log(`%c${logPrefix}%c ${message}`, 'color: #0099cc; font-weight: bold', 'color: inherit');
    } else {
      // Fallback for environments without color support
      console.log(`${logPrefix} ${message}`);
    }
  }
}

// This is fairly silly, but we need two versions of this file for main client code and for the
// service worker, apparently. The handler will comment and uncomment this stuff so that we can
// have both, as separate files GritsClient.js and GritsClient-sw.js:

// %MODULE%
export default GritsClient;

// %SERVICEWORKER%
//self.GritsClient = GritsClient;