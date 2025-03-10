/**
 * GritsClient - Client-side interface for interacting with the Grits file storage API
 * 
 * This class handles interactions with a Grits server, including:
 * - Path resolution through Merkle trees
 * - Caching of path to hash mappings
 * - Caching of metadata objects
 * - Content retrieval
 */
class GritsClient {
  /**
   * Create a new GritsClient
   * @param {Object} config - Configuration for the client
   * @param {string} config.serverUrl - Base URL of the Grits server, including protocol and port
   * @param {string} config.volume - The volume name to use on the server
   * @param {number} [config.metadataCacheExpiry=300000] - Metadata cache expiry in milliseconds (default 5 minutes)
   */
  constructor(config) {
    this.serverUrl = config.serverUrl.replace(/\/$/, ''); // Remove trailing slash if present
    this.volume = config.volume;
    this.metadataCacheExpiry = config.metadataCacheExpiry || 5 * 60 * 1000; // 5 minutes default

    // Cache for path to metadata hash mapping
    this.pathCache = new Map();
    
    // Cache for hash to JSON-decoded metadata - with time-based expiration
    this.metadataCache = new Map();
    
    // Content hash to size mapping
    this.contentSizeCache = new Map();
    
    // Root metadata hash
    this.rootHash = null;
    
    // Add timestamp for last server sync
    this.lastSync = 0;
  }

  /**
   * Clear all cached data
   */
  clearCache() {
    this.pathCache.clear();
    this.metadataCache.clear();
    this.contentSizeCache.clear();
    this.rootHash = null;
  }

  /**
   * Reset caching of the path space, forcing a reload on the next access
   */
  resetRoot() {
    this.rootHash = null;
    this.pathCache.clear();
  }

  /**
   * Fetch a file by path
   * @param {string} path - Path to the file
   * @returns {Promise<Blob>} - The file contents as a Blob
   */
  async fetchFile(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.contentHash) {
      throw new Error(`File not found: ${path}`);
    }
    
    return this._fetchBlob(pathInfo.contentHash);
  }

  /**
   * Look up a path and return its metadata
   * @param {string} path - Path to resolve
   * @returns {Promise<Object>} - Metadata for the path
   */
  async lookupPath(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.metadataHash) {
      throw new Error(`Path not found: ${path}`);
    }
    
    const metadata = await this._fetchMetadata(pathInfo.metadataHash);
    return metadata;
  }

  /**
   * Get the URL for a file by path
   * @param {string} path - Path to the file
   * @returns {Promise<string>} - URL that can be used to access the file
   */
  async getFileUrl(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.contentHash) {
      throw new Error(`File not found: ${path}`);
    }
    
    return `${this.serverUrl}/grits/v1/blob/${pathInfo.contentHash}`;
  }

  /**
   * List the contents of a directory
   * @param {string} path - Path to the directory
   * @returns {Promise<Array<{name: string, type: string, size: number}>>} - Directory contents
   */
  async listDirectory(path) {
    const normalizedPath = this._normalizePath(path);
    const pathInfo = await this._resolvePath(normalizedPath);
    
    if (!pathInfo || !pathInfo.metadataHash) {
      throw new Error(`Directory not found: ${path}`);
    }
    
    const metadata = await this._fetchMetadata(pathInfo.metadataHash);
    
    // If this is not a directory, throw an error
    if (metadata.type !== "dir") {
      throw new Error(`Not a directory: ${path}`);
    }
    
    // Fetch the directory content
    const contentHash = metadata.content_addr;
    const directoryContent = await this._fetchJson(contentHash);
    
    // Process the directory entries
    const entries = [];
    for (const [name, childMetadataHash] of Object.entries(directoryContent)) {
      try {
        const childMetadata = await this._fetchMetadata(childMetadataHash);
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
   * Resolve a path to its metadata and content hashes
   * @private
   * @param {string} path - Path to resolve
   * @returns {Promise<{metadataHash: string, contentHash: string, contentSize: number} | null>} - Hash information for the path
   */
  async _resolvePath(path) {
    // Check if we have this path in our cache
    if (this.pathCache.has(path)) {
      return this.pathCache.get(path);
    }
    
    // If not, we need to fetch it from the server
    try {
      const response = await fetch(`${this.serverUrl}/grits/v1/lookup/${this.volume}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(path)
      });
      
      if (!response.ok) {
        if (response.status === 404) {
          return null;
        }
        throw new Error(`Server error: ${response.status} ${response.statusText}`);
      }
      
      const pathSteps = await response.json();
      
      // Expected format: array of [partial path, metadata hash, content hash, content size]
      // Update our cache with all the path steps we got back
      for (const [stepPath, metadataHash, contentHash, contentSize] of pathSteps) {
        const pathInfo = {
          metadataHash,
          contentHash,
          contentSize
        };
        
        this.pathCache.set(stepPath, pathInfo);
        
        // Cache content size if available
        if (contentHash && contentSize) {
          this.contentSizeCache.set(contentHash, contentSize);
        }
        
        // If this is the root, store it
        if (stepPath === '') {
          this.rootHash = metadataHash;
        }
      }
      
      // Return the hash info for the requested path
      return this.pathCache.get(path) || null;
    } catch (err) {
      console.error(`Error resolving path ${path}:`, err);
      return null;
    }
  }

  /**
   * Fetch metadata for a hash
   * @private
   * @param {string} hash - Metadata hash to fetch
   * @returns {Promise<Object>} - Decoded metadata
   */
  async _fetchMetadata(hash) {
    // Check if we have this metadata in our cache
    const now = Date.now();
    if (this.metadataCache.has(hash)) {
      const cached = this.metadataCache.get(hash);
      if (now - cached.timestamp < this.metadataCacheExpiry) {
        return cached.data;
      }
      // Expired, remove from cache
      this.metadataCache.delete(hash);
    }
    
    // Fetch the metadata blob
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch metadata for ${hash}: ${response.status}`);
    }
    
    const metadata = await response.json();
    
    // Cache the metadata
    this.metadataCache.set(hash, {
      data: metadata,
      timestamp: now
    });
    
    return metadata;
  }

  /**
   * Fetch a blob by hash
   * @private
   * @param {string} hash - Hash to fetch
   * @returns {Promise<Blob>} - Blob content
   */
  async _fetchBlob(hash) {
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch blob ${hash}: ${response.status}`);
    }
    
    return response.blob();
  }

  /**
   * Fetch JSON data from a blob
   * @private
   * @param {string} hash - Hash to fetch
   * @returns {Promise<Object>} - Parsed JSON data
   */
  async _fetchJson(hash) {
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch JSON ${hash}: ${response.status}`);
    }
    
    return response.json();
  }

  /**
   * Normalize a path
   * @private
   * @param {string} path - Path to normalize
   * @returns {string} - Normalized path
   */
  _normalizePath(path) {
    // Remove leading/trailing slashes
    return path.replace(/^\/+|\/+$/g, '');
  }
}

export default GritsClient;