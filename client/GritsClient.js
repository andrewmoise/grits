/**
 * GritsClient - Client-side interface for interacting with the Grits file storage API
 */
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

    // Try fast path first
    const fastPathResult = await this._tryFastPathLookup(path);
    if (fastPathResult) {
      this.debugLog(path, "  fast path success");
      return fastPathResult;
    }
    
    // Check if there's already a request in flight for this path
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
    
    // Make the actual server request
    try {
      this.debugLog(path, "  start lookup fetch");

      const response = await fetch(`${this.serverUrl}/grits/v1/lookup/${this.volume}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(path)
      });
      
      if (!response.ok) {
        throw new Error(`Server error: ${response.status}`);
      }
      
      this.debugLog(path, "  parse response");

      const pathSteps = await response.json();
      
      this.debugLog(path, "  update direntry");

      // Update direntry cache with all the info we got back
      this._updateDirentryCacheFromLookup(pathSteps);
      
      this.debugLog(path, "  retry all in flight");

      // Try fast path for all in-flight requests
      this._retryAllInflightLookups();
      
      // If we get here and our request is still in the map, resolve it
      if (this.inflightLookups.has(path)) {
        this.debugLog(path, "  redo fast path");

        const result = await this._tryFastPathLookup(path); // FIXME -- I think this is not needed
        if (result) {
          this.debugLog(path, "  resolve");
          resolvePromise(result);
        } else {
          this.debugLog(path, "  reject");
          // This shouldn't happen since we just updated the cache
          rejectPromise(new Error(`Path resolution failed after lookup: ${path}`));
        }
        this.debugLog(path, "  delete");
        this.inflightLookups.delete(path);
      }
      
      this.debugLog(path, "  retry fast path AGAIN"); // FIXME FIXME
      return this._tryFastPathLookup(path);
    } catch (error) {
      this.debugLog(path, "  error");
      // If we get here and our request is still in the map, reject it
      if (this.inflightLookups.has(path)) {
        rejectPromise(error);
        this.inflightLookups.delete(path);
      }
      throw error;
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
  
    // TODO: Start background fetch requests for any hashes not in browser cache
  }

  // Retry fast path for all in-flight requests
  async _retryAllInflightLookups() {
    // Make a copy since we'll be modifying the map
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
    // Check if we have it in our cache
    if (this.jsonCache.has(hash)) {
      const entry = this.jsonCache.get(hash);
      // Update last accessed time
      entry.lastAccessed = Date.now();
      return entry.data;
    }
    
    // Try to get from browser cache
    try {
      const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`, {
        method: 'GET',
        cache: 'force-cache' // Try to use browser cache
      });
      
      if (!response.ok) {
        return null;
      }
      
      const data = await response.json();
      
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