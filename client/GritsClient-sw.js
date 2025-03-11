/**
 * Service worker compatible version of GritsClient
 * This assigns GritsClient to the global scope for service workers
 */

// This file is identical to GritsClient.js except it assigns to global scope
// instead of using export

class GritsClient {
  constructor(config) {
    this.serverUrl = config.serverUrl.replace(/\/$/, '');
    this.volume = config.volume;
    this.metadataCacheExpiry = config.metadataCacheExpiry || 5 * 60 * 1000;
    this.pathCache = new Map();
    this.metadataCache = new Map();
    this.contentSizeCache = new Map();
    this.rootHash = null;
    this.lastSync = 0;
  }
  
  // All methods exactly as in GritsClient.js
  clearCache() {
    this.pathCache.clear();
    this.metadataCache.clear();
    this.contentSizeCache.clear();
    this.rootHash = null;
  }
  
  resetRoot() {
    this.rootHash = null;
    this.pathCache.clear();
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
    
    const metadata = await this._fetchMetadata(pathInfo.metadataHash);
    return metadata;
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
    
    const metadata = await this._fetchMetadata(pathInfo.metadataHash);
    
    if (metadata.type !== "dir") {
      throw new Error(`Not a directory: ${path}`);
    }
    
    const contentHash = metadata.content_addr;
    const directoryContent = await this._fetchJson(contentHash);
    
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
  
  async _resolvePath(path) {
    if (this.pathCache.has(path)) {
      return this.pathCache.get(path);
    }
    
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
      
      for (const [stepPath, metadataHash, contentHash, contentSize] of pathSteps) {
        const pathInfo = {
          metadataHash,
          contentHash,
          contentSize
        };
        
        this.pathCache.set(stepPath, pathInfo);
        
        if (contentHash && contentSize) {
          this.contentSizeCache.set(contentHash, contentSize);
        }
        
        if (stepPath === '') {
          this.rootHash = metadataHash;
        }
      }
      
      return this.pathCache.get(path) || null;
    } catch (err) {
      console.error(`Error resolving path ${path}:`, err);
      return null;
    }
  }
  
  async _fetchMetadata(hash) {
    const now = Date.now();
    if (this.metadataCache.has(hash)) {
      const cached = this.metadataCache.get(hash);
      if (now - cached.timestamp < this.metadataCacheExpiry) {
        return cached.data;
      }
      this.metadataCache.delete(hash);
    }
    
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch metadata for ${hash}: ${response.status}`);
    }
    
    const metadata = await response.json();
    
    this.metadataCache.set(hash, {
      data: metadata,
      timestamp: now
    });
    
    return metadata;
  }
  
  async _fetchBlob(hash) {
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch blob ${hash}: ${response.status}`);
    }
    
    return response.blob();
  }
  
  async _fetchJson(hash) {
    const response = await fetch(`${this.serverUrl}/grits/v1/blob/${hash}`);
    
    if (!response.ok) {
      throw new Error(`Failed to fetch JSON ${hash}: ${response.status}`);
    }
    
    return response.json();
  }
  
  _normalizePath(path) {
    return path.replace(/^\/+|\/+$/g, '');
  }
}

// Assign to global scope for ServiceWorker to access
self.GritsClient = GritsClient;