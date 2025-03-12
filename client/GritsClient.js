/**
 * GritsClient - Client-side interface for interacting with the Grits file storage API
 */

const debugClientTiming = true;

class GritsClient {
  constructor(config) {
    // Basic configuration
    this.serverUrl = config.serverUrl.replace(/\/$/, '');
    this.volume = config.volume;

    // In-flight request management
    this.inflightLookups = new Map(); // Path → {promise, resolve, reject}
    this.inflightFetches = new Map(); // Hash → promise

    // Caching structures
    this.rootHash = null;
    this.rootHashTimestamp = 0; // When we last received the root hash
    this.jsonCache = new Map(); // Key: hash, Value: {data: Object, lastAccessed: timestamp}
    
    // Cache settings
    this.cacheMaxAge = 5 * 60 * 1000; // 5 minutes for less-used items
    this.softTimeout = config.softTimeout || 30 * 1000; // 30 seconds default
    this.hardTimeout = config.hardTimeout || 5 * 60 * 1000; // 5 minutes default
    
    // Setup periodic cleanup
    this.cleanupInterval = setInterval(() => this._cleanupCaches(), 5 * 60 * 1000);

    // Store service worker hash, for detection and reload when needed
    this.serviceWorkerHash = undefined;

    this.debugLog("constructor", `Initialized with soft timeout: ${this.softTimeout}ms, hard timeout: ${this.hardTimeout}ms`);
  }

  destroy() {
    clearInterval(this.cleanupInterval);
  }
  
  resetRoot() {
    this.rootHashTimestamp = 0;
    this.debugLog("resetRoot", "Root hash timestamp reset to 0");
  }
  
  async fetchFile(path) {
    const normalizedPath = this._normalizePath(path);
    
    // Check root hash age to determine strategy
    const now = Date.now();
    const rootAge = now - this.rootHashTimestamp;
    
    let result;
    
    if (!this.rootHash || rootAge > this.hardTimeout) {
      // Hard timeout exceeded - block and fetch fresh content
      this.debugLog(path, `Hard fetch needed (hash: ${this.rootHash}, age: ${rootAge}ms)`);
      result = await this._hardFetchContent(normalizedPath);
    } else {
      // We can fetch stuff quick, if we have it in cache
      const pathInfo = await this._tryFastPathLookup(normalizedPath);
      if (pathInfo && pathInfo.contentHash) {
        result = await this._fetchBlob(pathInfo.contentHash);
      } else {
        result = await this._hardFetchContent(normalizedPath);
      }
    }

    return result;
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

  // New method for hard fetching content
  async _hardFetchContent(path) {
    this.debugLog(path, `Hard fetching ${path} via /grits/v1/content`);
    
    // Check if we have a cached content hash for ETag
    let etag = null;
    const cachedValue = await this._tryFastPathLookup(path);
    if (cachedValue) {
      etag = `"${cachedValue.contentHash}"`;
      this.debugLog(path, `Using cached ETag: ${etag}`);
    }
    
    // Prepare headers
    const headers = {};
    if (etag) {
      headers['If-None-Match'] = etag;
    }
    
    // Make the request
    const method = 'GET';
    const url = `${this.serverUrl}/grits/v1/content/${this.volume}/${path}`;
    
    try {
      let response = await fetch(url, { method, headers });
      
      // Update service worker hash if available
      const newClientHash = response.headers.get('X-Grits-Service-Worker-Hash');
      if (newClientHash) {
        this.serviceWorkerHash = newClientHash;
      }

      this._prefetchMetadata(response);

      // 304 Not Modified - we use our cached version of the contents
      if (response.status === 304) {
        response = await this._fetchBlob(cachedValue.contentHash);
      }
        
      return response;
    } catch (error) {
      console.error(`Error during hard fetch for ${path}:`, error);
      throw error;
    }
  }
  
  // Prefetch metadata hashes that we don't have
  async _prefetchMetadata(response) {
    // Create an array of promises
    const prefetchPromises = [];
    
    // Get the JSON metadata
    const metadataJson = response.headers.get('X-Path-Metadata-JSON');
    if (!metadataJson) {
      this.debugLog("headers", "No X-Path-Metadata-JSON header found");
      return Promise.resolve();
    }
    
    try {
      // Parse the JSON data
      const pathMetadata = JSON.parse(metadataJson);
      this.debugLog("headers", `Parsed ${pathMetadata.length} path metadata entries`);
      
      // Process each entry
      for (const entry of pathMetadata) {
        const { component, path, hash } = entry;
        
        this.debugLog(path, `Processing metadata entry: component=${component}, hash=${hash}`);
        
        // Set root hash if this is the root entry (empty path)
        if (path === "") {
          this.debugLog("rootHash", `Updating root hash: ${hash}`);
          this.rootHash = hash;
          this.rootHashTimestamp = Date.now();
        }
        
        if (hash) {
          // Create a promise for prefetching this metadata
          const prefetchPromise = (async () => {
            try {
              // First fetch the metadata
              this.debugLog(`Prefetching ${path} metadata: ${hash}`);
              const metadata = await this._getJsonFromHash(hash, true);
              
              // Then fetch the content if it's available
              if (metadata && metadata.type == 'dir' && metadata.content_addr) {
                this.debugLog(`Prefetching ${path} content: ${metadata.content_addr}`);
                await this._getJsonFromHash(metadata.content_addr, true);
              }
            } catch (error) {
              console.error(`Couldn't prefetch ${path}: ${error.message}`);
            }
          })();
          
          prefetchPromises.push(prefetchPromise);
        }
      }
      
    } catch (error) {
      console.error(`Error parsing path metadata JSON: ${error.message}`);
      return Promise.resolve();
    }
    
    // Return a promise that resolves when all prefetches complete (or fail)
    return Promise.all(prefetchPromises).catch(error => {
      console.error(`Problem with batch prefetching: ${error}`);
    });
  }

  getServiceWorkerHash() {
    return this.serviceWorkerHash;
  }

  // Retry fast path for all in-flight requests
  async _retryAllInflightLookups() {
    // Make a copy, since we'll be modifying the map
    const entries = Array.from(this.inflightLookups.entries());
    
    for (const [path, request] of entries) {
      // Skip background refresh markers
      if (path.startsWith('bg:')) continue;
      
      try {
        const result = await this._tryFastPathLookup(path);
        if (result) {
          // This request can now be resolved!
          request.resolve(result);
          this.inflightLookups.delete(path);
        }
      } catch (error) {
        console.error(`Error retrying fast path for ${path}:`, error);
        // Don't reject here - let the original request handle errors
      }
    }
  }

  async _tryFastPathLookup(path) {
    // Can't do anything without a root hash
    if (!this.rootHash) {
      return null;
    }
    
    const components = path.split('/').filter(c => c.length > 0);
    let currentMetadataHash = this.rootHash;
    let currentContentHash = null;
    let currentContentSize = null;
    
    let triedIndexHtml = false;

    // Start with the root, then follow each path component
    for (let i = 0; i < components.length; i++) {
      const component = components[i];
      
      this.debugLog(path, `fast path component: ${component}`);

      this.debugLog(component, "parent metadata");

      // First, get the parent metadata
      const metadata = await this._getJsonFromHash(currentMetadataHash, false);
      if (!metadata || metadata.type !== 'dir') {
        return null; // Not a directory, can't continue
      }
      
      this.debugLog(component, "dir listing");

      // Get the directory listing
      const directoryListing = await this._getJsonFromHash(metadata.content_addr, false);
      if (!directoryListing || !directoryListing[component]) {
        return null; // Component not found in directory
      }
      
      this.debugLog(component, "child metadata");

      // Get child's metadata
      const childMetadataHash = directoryListing[component];
      const childMetadata = await this._getJsonFromHash(childMetadataHash, false);
      if (!childMetadata) {
        return null;
      }
      
      this.debugLog(component, "continue");

      // Continue with this child as the new current node
      currentMetadataHash = childMetadataHash;
      currentContentHash = childMetadata.content_addr;
      currentContentSize = childMetadata.size || 0;

      // Okay, this is a little weird... we check if we're about to return a bare directory, and if
      // so, we back up and do one more step with index.html and some hackery.
      if (i == components.length-1 && !triedIndexHtml) {
        triedIndexHtml = true;
        const currentMetadata = await this._getJsonFromHash(currentMetadataHash);
        if (currentMetadata.type != 'file') {
          components[i] = 'index.html';
          i--;
        }
      }
    }
    
    if (currentContentHash) {
      try {
        // Try to fetch from cache only
        const cacheResponse = await fetch(`${this.serverUrl}/grits/v1/blob/${currentContentHash}`, {
          method: 'HEAD', // HEAD is more efficient since we only need to check existence
          cache: 'only-if-cached',
          mode: 'same-origin'
        });
        
        // If not in cache, return null
        if (cacheResponse.status !== 200) {
          this.debugLog(path, `Content hash ${currentContentHash} not in browser cache, fallback to hard fetch`);
          return null;
        }
      } catch (error) {
        // Any error means it's not in cache
        this.debugLog(path, `Error checking cache for ${currentContentHash}: ${error.message}`);
        return null;
      }
    }

    // If we got here, we resolved the entire path
    return {
      metadataHash: currentMetadataHash,
      contentHash: currentContentHash,
      contentSize: currentContentSize
    };
  }

  async _getJsonFromHash(hash, forceFetch = false) {
    const startTime = performance.now();
    
    // Check if we have it in our memory cache
    if (this.jsonCache.has(hash)) {
      const entry = this.jsonCache.get(hash);
      // Update last accessed time
      entry.lastAccessed = Date.now();
      this.debugLog("hash:" + hash.substring(0, 8), `Memory cache hit (${Math.round(performance.now() - startTime)}ms)`);
      return entry.data;
    }
    
    this.debugLog("hash:" + hash.substring(0, 8), `Memory cache miss, trying browser cache - ${forceFetch}`);
    
    try {
      let response;
      const fetchStart = performance.now();
      
      if (!forceFetch) {
        // Only check browser cache, don't go to network
        try {
          response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`, {
            method: 'GET',
            cache: 'only-if-cached',
            mode: 'same-origin'
          });
          
          const fetchTime = Math.round(performance.now() - fetchStart);
          this.debugLog("hash:" + hash.substring(0, 8), `Cache fetch completed in ${fetchTime}ms, status ${response.status}`);
          
          // Cache miss should be handled by returning null
          if (response.status !== 200) {
            return null;
          }
        } catch (cacheError) {
          // Add more robust logging to see what's happening
          console.error("Cache-only fetch failed with error:", cacheError);
          this.debugLog("hash:" + hash.substring(0, 8), `Browser cache miss: ${cacheError.message}`);
          return null;
        }
      } 
      
      // Only do network request if we're forcing or we didn't return from cache miss above
      if (forceFetch) {
        const url = `${this.serverUrl}/grits/v1/blob/${hash}`;
        response = await fetch(url, {
          method: 'GET',
          cache: 'default' // Use standard browser caching behavior
        });
        
        const fetchTime = Math.round(performance.now() - fetchStart);
        this.debugLog("hash:" + hash.substring(0, 8), `Network fetch completed in ${fetchTime}ms, status ${response.status}`);
        
        if (!response.ok) {
          throw new Error(`Server returned ${response.status} for ${url}`);
        }
      }
      
      // If we're here, we have a valid response (either from cache or network)
      const jsonStart = performance.now();
      const data = await response.json();
      const jsonTime = Math.round(performance.now() - jsonStart);
      this.debugLog("hash:" + hash.substring(0, 8), `JSON parsing took ${jsonTime}ms`);
      
      // Cache it with current timestamp
      this.jsonCache.set(hash, {
        data,
        lastAccessed: Date.now()
      });
      
      return data;
    } catch (error) {
      console.error(`Failed to get JSON for ${hash}:`, error);
      return null;
    }
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
    
    // Clean content hash cache
    for (const [path, entry] of this.contentHashCache.entries()) {
      if (entry.timestamp < expiredTime) {
        this.contentHashCache.delete(path);
      }
    }
  }
  
  async _fetchBlob(hash) {
    // Check if there's already a fetch in progress for this hash
    if (this.inflightFetches.has(hash)) {
      this.debugLog(`hash:${hash.substring(0, 8)}`, "Using in-flight fetch");
      
      // Clone the response from the in-flight fetch
      const inFlightResponse = await this.inflightFetches.get(hash);
      return inFlightResponse.clone(); // Important: clone the response so it can be consumed multiple times
    }
    
    // Start a new fetch
    const fetchPromise = fetch(`${this.serverUrl}/grits/v1/blob/${hash}`).then(response => {
      // Store a cloned response that we'll return
      const clonedResponse = response.clone();
      
      // Remove from in-flight after a short delay to allow for near-simultaneous requests
      setTimeout(() => {
        this.inflightFetches.delete(hash);
      }, 100);
      
      return clonedResponse;
    });
    
    // Store the promise
    this.inflightFetches.set(hash, fetchPromise);
    
    return await fetchPromise;
  }

  _normalizePath(path) {
    return path.replace(/^\/+|\/+$/g, '');
  }
}

// This is fairly silly, but we need two versions of this file for main client code and for the
// service worker, apparently. The handler will comment and uncomment this stuff so that we can
// have both as separate files GritsClient.js and GritsClient-sw.js:

// %MODULE%
export default GritsClient;

// %SERVICEWORKER%
//self.GritsClient = GritsClient;