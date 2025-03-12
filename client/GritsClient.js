/**
 * GritsClient - Client-side interface for interacting with the Grits file storage API
 */

const debugClientTiming = false;

class GritsClient {
  constructor(config) {
    // Basic configuration
    this.serverUrl = config.serverUrl.replace(/\/$/, '');
    this.volume = config.volume;

    // In-flight request management
    this.inflightLookups = new Map(); // Path → {promise, resolve, reject}

    // Caching structures
    this.rootHash = null;
    this.direntryCache = new Map(); // Key: `${parentHash}:${component}`, Value: {metadataHash, contentHash, size}
    this.jsonCache = new Map(); // Key: hash, Value: {data: Object, lastAccessed: timestamp}
    
    // Cache settings
    this.cacheMaxAge = 5 * 60 * 1000; // 5 minutes for less-used items
    this.direntryCacheMaxSize = 5000; // Limit entries to prevent unbounded growth
    
    // Setup periodic cleanup
    this.cleanupInterval = setInterval(() => this._cleanupCaches(), 5 * 60 * 1000);

    // Store service worker hash, for detection and reload when needed
    this.serviceWorkerHash = undefined;
  }

  destroy() {
    clearInterval(this.cleanupInterval);
  }
  
  resetRoot() {
    this.rootHash = null;
    // We don't need to clear direntry cache - invalid entries will be replaced naturally
  }
  
  async fetchFile(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.contentHash) {
      throw new Error(`File not found: ${path}`);
    }
    
    return this._fetchBlob(pathInfo.contentHash);
  }
  
  async lookupPath(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.metadataHash) {
      throw new Error(`Path not found: ${path}`);
    }
    
    // Use the unified JSON fetching method
    return this._getJsonFromHash(pathInfo.metadataHash);
  }
  
  async getFileUrl(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.contentHash) {
      throw new Error(`File not found: ${path}`);
    }
    
    return `${this.serverUrl}/grits/v1/blob/${pathInfo.contentHash}`;
  }
  
  async listDirectory(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.metadataHash) {
      throw new Error(`Directory not found: ${path}`);
    }
    
    // Get directory metadata
    const metadata = await this._getJsonFromHash(pathInfo.metadataHash);
    
    if (metadata.type !== "dir") {
      throw new Error(`Not a directory: ${path}`);
    }
    
    // Get directory content listing
    const contentHash = metadata.content_addr;
    const directoryContent = await this._getJsonFromHash(contentHash);
    
    // Build the entries list
    const entries = [];
    for (const [name, childMetadataHash] of Object.entries(directoryContent)) {
      try {
        const childMetadata = await this._getJsonFromHash(childMetadataHash);
        entries.push({
          name,
          type: childMetadata.type,
          size: childMetadata.size || 0
        });
      } catch (err) {
        console.warn(`Failed to fetch metadata for ${name}: ${err}`);
      }
    }
    
    return entries;
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

  // Example usage:
  // debugLog('/path/to/file.js', 'Processing file');
  // debugLog('/api/users/123', 'User data received', { color: false });
  // debugLog('/volumes/data/images/photo.jpg', 'Image loaded', { prefix: 'MEDIA', includeDate: true });

  // Main path resolution function
  async _resolvePath(path) {
    this.debugLog(path, "Start lookup");

    // Try synchronous path first
    if (this.rootHash) {
      const quickResult = this._synchronousCacheLookup(path);
      if (quickResult) {
        this.debugLog(path, "  ultra-fast path success");
        return quickResult;
      }
    }

    // Try "fast" path fetching from browser cache, if that doesn't work
    const fastPathResult = await this._tryFastPathLookup(path);
    if (fastPathResult) {
      this.debugLog(path, "  fast path success");
      return fastPathResult;
    }
    
    // Check if there's already a request in flight for this path
    // FIXME -- Technically, we should check if any child of `path` is in there also,
    // but that's *very* rare, probably actually not worth worrying about
    if (this.inflightLookups.has(path)) {
      this.debugLog(path, "  already in flight");
      return this.inflightLookups.get(path).promise;
    }
    
    // Create a new promise for this request
    let resolvePromise, rejectPromise;
    const promise = new Promise((resolve, reject) => {
      resolvePromise = resolve;
      rejectPromise = reject;
    });
    
    // Add to in-flight requests
    this.inflightLookups.set(path, {
      promise,
      resolve: resolvePromise,
      reject: rejectPromise
    });
    
    this._fetchAndResolve(path);

    return promise;
  }

  getServiceWorkerHash() {
    return this.serviceWorkerHash
  }

  async _fetchAndResolve(path) {
    try {
        this.debugLog(path, "  start lookup fetch");
        const response = await fetch(`${this.serverUrl}/grits/v1/lookup/${this.volume}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(path)
        });
        
        const newClientHash = response.headers.get('X-Grits-Service-Worker-Hash');
        if (newClientHash) {
          this.serviceWorkerHash = newClientHash
        }

        if (response.status === 200) {
            // Full path found - process normally
            const pathSteps = await response.json();
            this._updateDirentryCacheFromLookup(pathSteps);
            this._retryAllInflightLookups();
        } else if (response.status === 207) {
            // Partial path found - the precise path we requested doesn't actually exist.
            const pathSteps = await response.json();
            this._updateDirentryCacheFromLookup(pathSteps);
            
            // FIXME - we should really cache some negative responses for *all* intermediate paths

            // FIXME - this doesn't cache the negative result until we start to
            // prefetch any directory content blobs we don't have; we won't actually be able to
            // find this result again, and we'll have to do another fetch and fail the next time
            // someone requests this path.

            // Extract the last valid path step to determine where the path broke
            const lastValidStep = pathSteps[pathSteps.length - 1];
            const lastValidPath = lastValidStep[0]; // Path string
            
            // Note! We could do a version of this, at this point, with a negative result cached.
            // The only reason we're not is (a) it's not necessary since once we start getting blobs,
            // we'll be able to quickly return failure anyway (b) it's a little fiddly to sync
            // up the split-up original path with the result steps from pathSteps.

            //this.direntryCache.set(direntryCacheKey, {
            //  status: 207
            //});

            // Create a 404 error for the original path
            const error = new Error(`Path not found: ${path}`);
            error.status = 404;
            error.partialPath = lastValidPath;
            
            // Reject the original request
            if (this.inflightLookups.has(path)) {
                this.inflightLookups.get(path).reject(error);
                this.inflightLookups.delete(path);
            }
            
            // We could try to resolve any others that might also be satisfied, but again, that
            // won't actually accomplish anything until we start grabbing the actual blobs in question
            //this._retryAllInflightLookups();
        } else {
            // Some other error occurred
            const error = new Error(`Server error: ${response.status}`);
            error.status = response.status;
            
            // Reject the original request
            if (this.inflightLookups.has(path)) {
                this.inflightLookups.get(path).reject(error);
                this.inflightLookups.delete(path);
            }
        }
    } catch (error) {
        // Network error or other exception
        if (this.inflightLookups.has(path)) {
            this.inflightLookups.get(path).reject(error);
            this.inflightLookups.delete(path);
        }
    }
  }
  
  // Update direntry cache with lookup results
  _updateDirentryCacheFromLookup(pathSteps) {
    for (const [stepPath, metadataHash, contentHash, contentSize] of pathSteps) {
      // If it's the root, update rootHash
      if (stepPath === '') {
        this.rootHash = metadataHash;
        continue;
      }
      
      // For each step, we need to compute the parent path and the component name
      const lastSlashIndex = stepPath.lastIndexOf('/');
      const parentPath = lastSlashIndex > 0 ? stepPath.substring(0, lastSlashIndex) : '';
      const component = stepPath.substring(lastSlashIndex + 1);
      
      // For the direntry cache, we need the parent's metadata hash
      // We can find it by looking at the previous entries in pathSteps
      for (const [candidatePath, candidateMetadataHash] of pathSteps) {
        if (candidatePath === parentPath) {
          // Found the parent's metadata hash
          const direntryCacheKey = `${candidateMetadataHash}:${component}`;
          this.direntryCache.set(direntryCacheKey, {
            metadataHash,
            contentHash,
            contentSize
          });
          break;
        }
      }
    }
  
    // TODO: Start background fetch requests for any hashes not in browser cache, so that we'll
    // get serialized stuff we can use in the future or from other service workers in other tabs
  }

  // Retry fast path for all in-flight requests
  async _retryAllInflightLookups() {
    // Make a copy, since we'll be modifying the map
    const entries = Array.from(this.inflightLookups.entries());
    
    for (const [path, request] of entries) {
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

  _synchronousCacheLookup(path) {
    // Only attempt if we have a root hash
    if (!this.rootHash) return null;
    
    const components = path.split('/').filter(c => c.length > 0);
    let currentMetadataHash = this.rootHash;
    let currentContentHash = null;
    let currentContentSize = null;
    
    // Try to follow path using only in-memory cache
    for (let i = 0; i < components.length; i++) {
      const component = components[i];
      const direntryCacheKey = `${currentMetadataHash}:${component}`;
      
      // If not in direntry cache, abort the fast path
      if (!this.direntryCache.has(direntryCacheKey)) {
        return null;
      }
      
      const entry = this.direntryCache.get(direntryCacheKey);
      currentMetadataHash = entry.metadataHash;
      currentContentHash = entry.contentHash;
      currentContentSize = entry.contentSize;
    }
    
    return {
      metadataHash: currentMetadataHash,
      contentHash: currentContentHash,
      contentSize: currentContentSize
    };
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
    
    // Start with the root, then follow each path component
    for (let i = 0; i < components.length; i++) {
      const component = components[i];
      const direntryCacheKey = `${currentMetadataHash}:${component}`;
      
      // Try the direntry cache first
      if (this.direntryCache.has(direntryCacheKey)) {
        const entry = this.direntryCache.get(direntryCacheKey);
        currentMetadataHash = entry.metadataHash;
        currentContentHash = entry.contentHash;
        currentContentSize = entry.contentSize;
        continue;
      }
      
      // Not in direntry cache - try to resolve it by checking metadata and directory contents
      
      // First, get the parent metadata
      const metadata = await this._getJsonFromHash(currentMetadataHash);
      if (!metadata || metadata.type !== 'dir') {
        return null; // Not a directory, can't continue
      }
      
      // Get the directory listing
      const directoryListing = await this._getJsonFromHash(metadata.content_addr);
      if (!directoryListing || !directoryListing[component]) {
        return null; // Component not found in directory
      }
      
      // Get child's metadata
      const childMetadataHash = directoryListing[component];
      const childMetadata = await this._getJsonFromHash(childMetadataHash);
      if (!childMetadata) {
        return null;
      }
      
      // Update the direntry cache
      this.direntryCache.set(direntryCacheKey, {
        metadataHash: childMetadataHash,
        contentHash: childMetadata.content_addr,
        contentSize: childMetadata.size || 0
      });
      
      // Continue with this child as the new current node
      currentMetadataHash = childMetadataHash;
      currentContentHash = childMetadata.content_addr;
      currentContentSize = childMetadata.size || 0;
    }
    
    // If we got here, we resolved the entire path
    return {
      metadataHash: currentMetadataHash,
      contentHash: currentContentHash,
      contentSize: currentContentSize
    };
  }

  async _getJsonFromHash(hash) {
    const startTime = performance.now();
    
    // Check if we have it in our cache
    if (this.jsonCache.has(hash)) {
      const entry = this.jsonCache.get(hash);
      // Update last accessed time
      entry.lastAccessed = Date.now();
      this.debugLog("hash:" + hash.substring(0, 8), `Memory cache hit (${Math.round(performance.now() - startTime)}ms)`);
      return entry.data;
    }
    
    this.debugLog("hash:" + hash.substring(0, 8), "Memory cache miss, trying browser cache");
    
    // Try to get from browser cache
    try {
      const fetchStart = performance.now();
      const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`, {
        method: 'GET',
        cache: 'force-cache' // Try to use browser cache
      });
      
      const fetchTime = Math.round(performance.now() - fetchStart);
      this.debugLog("hash:" + hash.substring(0, 8), `Fetch completed in ${fetchTime}ms`);
      
      if (!response.ok) {
        return null;
      }
      
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
    
    // Limit direntry cache size to prevent unbounded growth
    if (this.direntryCache.size > this.direntryCacheMaxSize) {
      console.log(`Pruning direntry cache (size: ${this.direntryCache.size})`);
      // Simple approach: delete oldest half
      // In a real implementation, you might want a more sophisticated LRU approach
      const entries = Array.from(this.direntryCache.entries());
      const toDelete = entries.slice(0, Math.floor(entries.length / 2));
      for (const [key] of toDelete) {
        this.direntryCache.delete(key);
      }
    }
  }
  
  async _fetchBlob(hash) {
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch blob ${hash}: ${response.status}`);
    }
    
    return response.blob();
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