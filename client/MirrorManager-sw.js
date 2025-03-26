/**
 * MirrorManager - Client-side class for managing connections with content mirrors
 */

const debugMirrors = true;

class MirrorManager {
  constructor(config = {}) {
    // Mirror configuration
    this.serverUrl = config.serverUrl?.replace(/\/$/, '');
    this.volume = config.volume;
    this.mirrors = [];
    this.mirrorPerformance = new Map(); // Map of mirror URL to performance metrics

    // For storing completed requests before processing
    this.completedTrackingObjects = [];

    this.refreshInterval = config.refreshInterval || 5 * 60 * 1000; // 5 minutes for mirror list refresh
    
    // Request tracking
    this.activeRequests = 0;
    this.maxConcurrentRequests = config.maxConcurrentRequests || 6; // Default max parallel requests per mirror
    
    // Stats tracking
    this.totalBytesReceived = new Map(); // Track bytes received per mirror
    //this.requestsPerMirror = new Map(); // Track requests per mirror

    // Constants for selection strategy
    this.ACTIVE_REQUEST_THRESHOLD = 4; // When to start using less-tested mirrors
    this.RECENT_COMMUNICATION_THRESHOLD = 60 * 1000; // 1 minute
    this.OLD_COMMUNICATION_THRESHOLD = 10 * 60 * 1000; // 10 minutes

    // Setup cleanup intervals if we have a server URL
    if (this.serverUrl) {
      this.mirrorRefreshTimer = setInterval(() => this.refreshMirrorList(), this.refreshInterval);
    }
  }

  /**
   * Initialize the mirror manager with the list of available mirrors
   */
  async initialize() {
    if (!this.serverUrl) {
      this.debugLog("Cannot initialize: missing server URL");
      return false;
    }
    
    try {
      await this.refreshMirrorList();
      return true;
    } catch (error) {
      console.error("Failed to initialize mirror manager:", error);
      return false;
    }
  }

/**
 * Refresh the list of available mirrors from the server
 */
async refreshMirrorList() {
  if (!this.serverUrl) {
    this.debugLog("Cannot refresh mirror list: missing server URL");
    return;
  }
  
  try {
    const url = `${this.serverUrl}/grits/v1/origin/list-mirrors`;
    this.debugLog(`Refreshing mirror list from ${url}`);
    
    const response = await fetch(url, {
      method: 'GET',
      headers: { 'Accept': 'application/json' }
    });

    if (response.status == 404) {
      // Nothing's really wrong; maybe they just aren't running the origin module.
      this.mirrors = [];
      return;
    }
    
    if (!response.ok) {
      // This, though, might indicate an actual problem.
      console.error(`Failed to fetch mirror list: ${response.status}`);
      return;
    }

    const mirrorList = await response.json();
    
    // Log the first mirror for debugging
    if (mirrorList.length > 0) {
      this.debugLog(`Mirror sample: ${JSON.stringify(mirrorList[0])}`);
    }
    
    // Update the mirrors list
    this.mirrors = mirrorList.map(mirror => {
      // The mirror.url is now the complete URL string
      const url = mirror.url?.replace(/\/$/, '') || null; // Remove trailing slash if present
      
      return {
        id: url, // Use the URL as the ID for simplicity
        url: url,
        volumes: Array.isArray(mirror.volumes) ? mirror.volumes : [this.volume]
      };
    }).filter(mirror => mirror.url); // Filter out any mirrors with invalid URLs

    //this.debugLog(`Updated mirror list, found ${this.mirrors.length} mirrors`);
    //console.log(`Raw mirror list: ${JSON.stringify(mirrorList)}`);
    
    // Initialize performance metrics for any new mirrors
    for (const mirror of this.mirrors) {
      if (!this.mirrorPerformance.has(mirror.url)) {
        this.mirrorPerformance.set(mirror.url, {
          ttfb: { // Time to first byte
            values: [],
            average: 0
          },
          bandwidth: { // In bytes/sec
            values: [],
            average: 0
          },
          lastCommunicated: 0,
          errorCount: 0,
          successCount: 0,
          totalRequests: 0,
          inFlightRequests: 0
        });
      }
      
      // Initialize stats counters
      if (!this.totalBytesReceived.has(mirror.url)) {
        this.totalBytesReceived.set(mirror.url, 0);
      }
      
      //if (!this.requestsPerMirror.has(mirror.url)) {
      //  this.requestsPerMirror.set(mirror.url, 0);
      //}
    }
    
    return this.mirrors;
  } catch (error) {
    console.error('Error refreshing mirror list:', error);
    return [];
  }
}

  /**
   * Select the best mirror based on current performance metrics and load
   * @param {string} hash - The content hash being requested
   * @returns {string} The URL of the selected mirror, or null if none available
   */
  selectBestMirror(hash) {
    // Check if we have any mirrors configured
    if (this.mirrors.length === 0) {
      this.debugLog(`No mirrors available`);
      return null;
    }
    
    // Filter mirrors that serve this volume
    const compatibleMirrors = this.mirrors.filter(mirror => 
      mirror.volumes.includes(this.volume)
    );
    
    if (compatibleMirrors.length === 0) {
      this.debugLog(`No compatible mirrors found for volume ${this.volume}`);
      return null;
    }
    
    // Strategy:
    // 1. If not many active requests, use mirror with good metrics
    // 2. If many active requests, try mirrors we haven't used recently
    
    const now = Date.now();
    const highLoadMode = this.activeRequests >= this.ACTIVE_REQUEST_THRESHOLD;
    
    // Group mirrors by their recency of communication
    const recentMirrors = [];
    const oldMirrors = [];
    
    for (const mirror of compatibleMirrors) {
      const stats = this.mirrorPerformance.get(mirror.url);
      if (!stats) continue;
      
      const timeSinceLastUse = now - stats.lastCommunicated;
      
      if (timeSinceLastUse < this.RECENT_COMMUNICATION_THRESHOLD) {
        recentMirrors.push({ mirror, stats });
      } else if (timeSinceLastUse > this.OLD_COMMUNICATION_THRESHOLD) {
        oldMirrors.push({ mirror, stats });
      }
    }
    
    // If under high load and we have old mirrors, use one of those
    if (highLoadMode && oldMirrors.length > 0) {
      // Pick the old mirror with the lowest error count
      oldMirrors.sort((a, b) => a.stats.errorCount - b.stats.errorCount);
      return oldMirrors[0].mirror.url;
    }
    
    // Otherwise, use a recent mirror with good metrics
    if (recentMirrors.length > 0) {
      // Score mirrors by a combination of TTFB, bandwidth, and current load
      const scoredMirrors = recentMirrors.map(({ mirror, stats }) => {
        // Normalize values between 0-1 (lower is better for TTFB, higher is better for bandwidth)
        const ttfbScore = stats.ttfb.average ? 1000 / Math.max(stats.ttfb.average, 100) : 0.5;
        const bandwidthScore = stats.bandwidth.average ? Math.min(stats.bandwidth.average / 1000000, 1) : 0.5;
        const loadScore = 1 - (stats.inFlightRequests / this.maxConcurrentRequests);
        
        // Weighted score (adjust weights as needed)
        const score = (ttfbScore * 0.4) + (bandwidthScore * 0.4) + (loadScore * 0.2);
        
        return { mirror, score };
      });
      
      // Sort by score (highest first)
      scoredMirrors.sort((a, b) => b.score - a.score);
      return scoredMirrors[0].mirror.url;
    }
    
    // Fall back to least loaded compatible mirror
    const mirrorsByLoad = compatibleMirrors
      .map(mirror => ({ 
        mirror, 
        load: this.mirrorPerformance.get(mirror.url)?.inFlightRequests || 0 
      }))
      .sort((a, b) => a.load - b.load);
    
    return mirrorsByLoad[0].mirror.url;
  }

  /**
   * Fetch content from the best available mirror
   * @param {string} hash - The content hash
   * @param {string} [extension] - Optional file extension
   * @returns {Promise<Response>} The fetch response
   */
  async fetchBlob(hash, extension = null) {
    // If no mirrors are available, fall back to the server
    if (this.mirrors.length === 0) {
      try {
        let url = `${this.serverUrl}/grits/v1/blob/${hash}`;
        if (extension) {
          url += `.${extension}`;
        }
        return await fetch(url);
      } finally {
        this.activeRequests--;
      }
    }

    // Create a tracking object for this request
    const trackingObj = {
      url: null,
      startTime: performance.now(),
      bytesReceived: 0,
      ttfb: 0,
      success: false,
      requestComplete: false,
      bandwidthMeasured: false
    };
    
    this.activeRequests++;
    
    // Try to fetch from a mirror
    try {
      const mirrorUrl = this.selectBestMirror(hash);
      if (!mirrorUrl) {
        throw new Error(`No suitable mirror found for ${this.volume}/${hash}`);
      }
      if (debugMirrors) {
        console.log(`Best mirror: ${mirrorUrl}`);
      }
  
      // Set the URL in our tracking object
      trackingObj.url = mirrorUrl;

      // Construct the full URL
      let url = `${mirrorUrl}/grits/v1/blob/${hash}`;
      if (extension) {
        url += `.${extension}`;
      }

      // Update mirror stats
      const stats = this.mirrorPerformance.get(mirrorUrl);
      stats.inFlightRequests++;
      stats.totalRequests++;
      //this.requestsPerMirror.set(mirrorUrl, (this.requestsPerMirror.get(mirrorUrl) || 0) + 1);
      
      // Start timing
      const startTime = performance.now();
      
      if (debugMirrors) {
        console.log(`About to execute fetch for ${url}`);
      }
      const response = await fetch(url);
      if (debugMirrors) {
        console.log(`Fetch completed with status: ${response.status}, ok: ${response.ok}`);
      }

      // Record TTFB
      trackingObj.ttfb = performance.now() - trackingObj.startTime;

      if (!response.ok) {
        console.error(`Error return from mirror fetch: ${response.status}`);
        stats.errorCount++;
        throw new Error(`Mirror returned status ${response.status}`);
      }
      
      if (debugMirrors) {
        console.log(`Successfully fetched from mirror: ${url}`);
      }
            
      // Clone the response for bandwidth measurement
      const clonedResponse = response.clone();
      
      // Measure bandwidth in the background (which will add the tracking obj to stats, also)
      this.measureBandwidth(clonedResponse, trackingObj);

      stats.successCount++;
      return response;
    } catch (error) {
      trackingObj.success = false;
      trackingObj.requestComplete = true;
      this.completedTrackingObjects.push(trackingObj);

      console.error(`Error fetching ${hash} from mirror: ${error.message}`);
      console.error(`Stack: ${error.stack}`);
           
      // Fall back to server if mirror fails
      let url = `${this.serverUrl}/grits/v1/blob/${hash}`;
      if (extension) {
        url += `.${extension}`;
      }
      return await fetch(url);
    } finally {
      this.activeRequests--;
    }
  }

  /**
   * Measure bandwidth from a response
   * @param {string} mirrorUrl - The mirror URL
   * @param {Response} response - The cloned response
   * @param {number} startTime - The start time in milliseconds
   */
  async measureBandwidth(response, trackingObj) {
    try {
      const mirrorUrl = trackingObj.url;
      if (!mirrorUrl) {
        console.warn("No mirror URL in tracking object");
        return;
      }
      
      console.log(`Starting bandwidth measurement for ${mirrorUrl}`);
      
      // Try to get content length first
      const contentLength = response.headers.get('content-length');
      if (contentLength) {
        trackingObj.bytesReceived = parseInt(contentLength, 10);
        console.log(`Content-Length header indicates ${trackingObj.bytesReceived} bytes`);
      } else {
        console.log(`No Content-Length header for ${mirrorUrl}`);
      }
      
      // Check if response body is available
      if (!response.body) {
        console.warn(`No response body available for ${mirrorUrl}`);
        trackingObj.requestComplete = true;
        this.completedTrackingObjects.push(trackingObj);
        return;
      }
      
      // Read the response to measure actual bytes and bandwidth
      const reader = response.body.getReader();
      let bytesReceived = 0;
      const startMeasureTime = performance.now();
      let chunkCount = 0;
      
      try {
        while (true) {
          const {done, value} = await reader.read();
          
          if (done) {
            console.log(`Done reading after ${chunkCount} chunks, ${bytesReceived} total bytes`);
            break;
          }
          
          chunkCount++;
          bytesReceived += value.length;
          
          if (chunkCount === 1) {
            console.log(`First chunk received: ${value.length} bytes`);
          }
          
          // Check if we've measured enough
          const measureDuration = performance.now() - startMeasureTime;
          if (measureDuration > 500 || bytesReceived > 50000) {
            console.log(`Reached measurement limit after ${chunkCount} chunks and ${bytesReceived} bytes (${measureDuration.toFixed(2)}ms)`);
            // Calculate bandwidth
            const bandwidthBps = (bytesReceived / measureDuration) * 1000;
            this.recordBandwidth(mirrorUrl, bandwidthBps);
            break;
          }
        }
      } catch (readError) {
        console.error(`Error reading response body: ${readError}`);
      }
      
      console.log(`Read completed, received ${bytesReceived} bytes`);
      
      // Use the greater of content-length or actual bytes measured
      trackingObj.bytesReceived = Math.max(trackingObj.bytesReceived, bytesReceived);
      console.log(`Final byte count for ${mirrorUrl}: ${trackingObj.bytesReceived}`);
  
      // Mark as complete
      trackingObj.success = true;
      trackingObj.bandwidthMeasured = true;
      trackingObj.requestComplete = true;
  
      this.completedTrackingObjects.push(trackingObj);
      console.log(`Added successful tracking object for ${mirrorUrl}`);
  
    } catch (error) {
      console.warn(`Error in measureBandwidth: ${error.message}\n${error.stack}`);
      // Still mark as complete even on error
      trackingObj.requestComplete = true;
      this.completedTrackingObjects.push(trackingObj);
      console.log(`Added failed tracking object for ${trackingObj.url || 'unknown'}`);
    }
  }

  /**
   * Record bandwidth measurement for a mirror
   * @param {string} mirrorUrl - The mirror URL
   * @param {number} bandwidthBps - Bandwidth in bytes per second
   */
  recordBandwidth(mirrorUrl, bandwidthBps) {
    const stats = this.mirrorPerformance.get(mirrorUrl);
    if (!stats) return;
    
    stats.bandwidth.values.push(bandwidthBps);
    
    // Keep only the last 10 measurements
    if (stats.bandwidth.values.length > 10) {
      stats.bandwidth.values.shift();
    }
    
    // Update average
    stats.bandwidth.average = stats.bandwidth.values.reduce((sum, val) => sum + val, 0) / stats.bandwidth.values.length;
  }

  /**
   * Reset old statistics to prevent stale data from affecting decisions
   */
  resetOldStatistics() {
    const now = Date.now();
    
    for (const [url, stats] of this.mirrorPerformance.entries()) {
      // If we haven't communicated with this mirror in a while, reset its stats
      if (now - stats.lastCommunicated > 30 * 60 * 1000) { // 30 minutes
        this.mirrorPerformance.set(url, {
          ttfb: { values: [], average: 0 },
          bandwidth: { values: [], average: 0 },
          lastCommunicated: 0,
          errorCount: 0,
          successCount: 0,
          totalRequests: 0,
          inFlightRequests: 0
        });
      }
    }
  }

  /**
   * Get statistics about all mirrors
   * @returns {Array} Array of mirror statistics
   */
  getMirrorStats() {
    this.processCompletedTrackingObjects();

    const stats = [];
    
    for (const mirror of this.mirrors) {
      const perfStats = this.mirrorPerformance.get(mirror.url);
      if (!perfStats) continue;
      
      const reliability = perfStats.totalRequests > 0 
        ? ((perfStats.successCount / perfStats.totalRequests) * 100).toFixed(2)
        : 'N/A';
      
      // Get raw bytes value
      const rawBytes = this.totalBytesReceived.get(mirror.url) || 0;
      
      stats.push({
        url: mirror.url,
        latency: perfStats.ttfb.average.toFixed(2) + ' ms',
        bandwidth: (perfStats.bandwidth.average / 1024).toFixed(2) + ' KB/s',
        reliability: reliability + '%',
        //requests: this.requestsPerMirror.get(mirror.url) || 0,
        bytesFetched: this.formatBytes(rawBytes),
        rawBytes: rawBytes // Add raw numeric value for comparison
      });
    }
    
    return stats;
  }

  processCompletedTrackingObjects() {
    if (this.completedTrackingObjects.length === 0) {
      return;
    }
    
    console.log(`Processing ${this.completedTrackingObjects.length} completed tracking objects`);
    
    // First, print a summary of unprocessed tracking objects
    this.completedTrackingObjects.forEach((obj, index) => {
      console.log(`[Tracking #${index}] URL: ${obj.url}, Bytes: ${obj.bytesReceived}, Success: ${obj.success}`);
    });
    
    // Then update the master performance metrics
    for (const obj of this.completedTrackingObjects) {
      if (!obj.url) continue;
      
      // Update total bytes received
      if (obj.bytesReceived > 0) {
        this.totalBytesReceived.set(
          obj.url, 
          (this.totalBytesReceived.get(obj.url) || 0) + obj.bytesReceived
        );
      }
      
      // Update TTFB (we could also maintain this in the recordTTFB method)
      if (obj.ttfb > 0) {
        const stats = this.mirrorPerformance.get(obj.url);
        if (stats) {
          stats.ttfb.values.push(obj.ttfb);
          if (stats.ttfb.values.length > 10) {
            stats.ttfb.values.shift();
          }
          stats.ttfb.average = stats.ttfb.values.reduce((sum, val) => sum + val, 0) / stats.ttfb.values.length;
        }
      }
    }
    
    // Clear the list
    this.completedTrackingObjects = [];
  }

  /**
   * Reset the statistics counters
   */
  resetStats() {
    //for (const url of this.requestsPerMirror.keys()) {
    //  this.requestsPerMirror.set(url, 0);
    //}
    
    for (const url of this.totalBytesReceived.keys()) {
      this.totalBytesReceived.set(url, 0);
    }
  }

  /**
   * Format bytes to human-readable format
   * @param {number} bytes - Number of bytes
   * @returns {string} Formatted string
   */
  formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  /**
   * Debug log function
   * @param {string} message - Message to log
   */
  debugLog(message) {
    if (debugMirrors) {
      console.log(`[MirrorManager] ${message}`);
    }
  }

  /**
   * Clean up resources
   */
  destroy() {
    if (this.mirrorRefreshTimer) {
      clearInterval(this.mirrorRefreshTimer);
    }
    if (this.statsResetTimer) {
      clearInterval(this.statsResetTimer);
    }
  }
}

// Export, in stupid serviceworker-appropriate form
self.MirrorManager = MirrorManager;