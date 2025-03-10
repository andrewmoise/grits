/**
 * GritsClientTests - Test suite for the GritsClient
 * 
 * This module provides automated tests for verifying the functionality
 * of the GritsClient library.
 */

class GritsClientTests {
  /**
   * Create a new test suite instance
   * @param {GritsClient} client - The client instance to test
   */
  constructor(client) {
    this.client = client;
    this.results = {
      passed: 0,
      failed: 0,
      tests: []
    };
  }

  /**
   * Run all tests in the suite
   * @returns {Promise<Object>} - Test results summary
   */
  async runAllTests() {
    console.log('🧪 Starting GritsClient test suite...');
    
    // Reset results
    this.results = {
      passed: 0,
      failed: 0,
      tests: []
    };
    
    // Define the tests to run
    const tests = [
      this.testRootAccess,
      this.testMetadataCache,
      this.testPathResolution,
      this.testFileContent,
      this.testListDirectory,
      this.testCacheClear
    ];
    
    // Run each test
    for (const test of tests) {
      await this._runTest(test.name, test.bind(this));
    }
    
    // Print summary
    console.log('📋 Test Summary:');
    console.log(`✅ Passed: ${this.results.passed}`);
    console.log(`❌ Failed: ${this.results.failed}`);
    
    return this.results;
  }
  
  /**
   * Test access to the root directory
   */
  async testRootAccess() {
    // Test that we can access the root metadata
    const rootMetadata = await this.client.lookupPath('');
    
    // Validate the root is a directory
    this._assert(
      'Root should be a directory',
      rootMetadata.type === 'dir',
      rootMetadata
    );
    
    // Check if content_addr exists
    this._assert(
      'Root should have content address',
      !!rootMetadata.content_addr,
      rootMetadata
    );
  }
  
  /**
   * Test the metadata caching functionality
   */
  async testMetadataCache() {
    // First request should populate the cache
    const firstRequest = await this.client.lookupPath('');
    
    // Get the metadata hash for the root
    const rootHash = this.client.rootHash;
    this._assert(
      'Root hash should be set after lookup',
      !!rootHash,
      rootHash
    );
    
    // Check that the metadata is cached
    this._assert(
      'Metadata should be cached',
      this.client.metadataCache.has(rootHash),
      Array.from(this.client.metadataCache.keys())
    );
    
    // Second request should use the cache
    const cacheEntry = this.client.metadataCache.get(rootHash);
    const beforeTimestamp = cacheEntry.timestamp;
    
    // Make a small delay
    await new Promise(resolve => setTimeout(resolve, 10));
    
    // Make second request
    const secondRequest = await this.client.lookupPath('');
    
    // Check that the cache timestamp hasn't changed
    const afterTimestamp = this.client.metadataCache.get(rootHash).timestamp;
    this._assert(
      'Cache should be used for second request',
      beforeTimestamp === afterTimestamp,
      { before: beforeTimestamp, after: afterTimestamp }
    );
  }
  
  /**
   * Test path resolution
   */
  async testPathResolution() {
    // Get a directory listing to find a child item
    const rootListing = await this.client.listDirectory('');
    
    // Skip test if root is empty
    if (rootListing.length === 0) {
      console.log('⚠️ Root directory is empty, skipping path resolution test');
      return;
    }
    
    // Find a file or directory to test with
    const testItem = rootListing[0];
    const itemPath = testItem.name;
    
    // Resolve the path
    const itemMetadata = await this.client.lookupPath(itemPath);
    
    // Verify we got metadata back
    this._assert(
      'Should get metadata for path item',
      !!itemMetadata,
      itemMetadata
    );
    
    // Check that type matches what was in the directory listing
    this._assert(
      'Item type should match directory listing',
      itemMetadata.type === testItem.type,
      { listingType: testItem.type, metadataType: itemMetadata.type }
    );
  }
  
  /**
   * Test file content retrieval
   */
  async testFileContent() {
    // Get a directory listing to find a file
    const rootListing = await this.client.listDirectory('');
    
    // Find a file to test with
    const testFile = rootListing.find(item => item.type === 'blob');
    
    // Skip test if no files found
    if (!testFile) {
      console.log('⚠️ No files found in root, skipping file content test');
      return;
    }
    
    const filePath = testFile.name;
    
    // Get the file URL
    const fileUrl = await this.client.getFileUrl(filePath);
    
    // Verify URL format
    this._assert(
      'File URL should be correctly formatted',
      fileUrl.startsWith(this.client.serverUrl) && fileUrl.includes('/blob/'),
      fileUrl
    );
    
    // Try to fetch the file
    const fileContent = await this.client.fetchFile(filePath);
    
    // Verify we got content back
    this._assert(
      'Should get file content',
      fileContent instanceof Blob,
      typeof fileContent
    );
    
    // Verify size matches metadata
    this._assert(
      'File size should match metadata',
      fileContent.size === testFile.size,
      { blobSize: fileContent.size, metadataSize: testFile.size }
    );
  }
  
  /**
   * Test directory listing
   */
  async testListDirectory() {
    // Get a directory listing
    const rootListing = await this.client.listDirectory('');
    
    // Verify we got an array
    this._assert(
      'Directory listing should be an array',
      Array.isArray(rootListing),
      rootListing
    );
    
    // Find a subdirectory to test with
    const subdir = rootListing.find(item => item.type === 'dir');
    
    // Skip test if no subdirectories found
    if (!subdir) {
      console.log('⚠️ No subdirectories found in root, skipping subdirectory listing test');
      return;
    }
    
    // List the subdirectory
    const subdirPath = subdir.name;
    const subdirListing = await this.client.listDirectory(subdirPath);
    
    // Verify we got an array
    this._assert(
      'Subdirectory listing should be an array',
      Array.isArray(subdirListing),
      subdirListing
    );
  }
  
  /**
   * Test cache clearing
   */
  async testCacheClear() {
    // Ensure we have something in the cache
    await this.client.lookupPath('');
    
    // Check that caches have content
    const pathCacheSize = this.client.pathCache.size;
    const metadataCacheSize = this.client.metadataCache.size;
    
    this._assert(
      'Path cache should not be empty before clear',
      pathCacheSize > 0,
      pathCacheSize
    );
    
    this._assert(
      'Metadata cache should not be empty before clear',
      metadataCacheSize > 0,
      metadataCacheSize
    );
    
    // Clear the cache
    this.client.clearCache();
    
    // Verify caches are now empty
    this._assert(
      'Path cache should be empty after clear',
      this.client.pathCache.size === 0,
      this.client.pathCache.size
    );
    
    this._assert(
      'Metadata cache should be empty after clear',
      this.client.metadataCache.size === 0,
      this.client.metadataCache.size
    );
    
    this._assert(
      'Root hash should be null after clear',
      this.client.rootHash === null,
      this.client.rootHash
    );
  }
  
  /**
   * Helper to run an individual test
   * @private
   */
  async _runTest(testName, testFn) {
    console.log(`\n▶️ Running test: ${testName}`);
    
    const testResult = {
      name: testName,
      assertions: [],
      passed: true
    };
    
    try {
      // Set up a test context to collect assertions
      this._currentTest = testResult;
      
      // Run the test
      await testFn();
      
      // If we got here, the test didn't throw
      if (testResult.passed) {
        console.log(`✅ ${testName} passed`);
        this.results.passed++;
      } else {
        console.log(`❌ ${testName} failed`);
        this.results.failed++;
      }
    } catch (error) {
      // Test threw an exception
      console.error(`❌ ${testName} error:`, error);
      testResult.error = error.message;
      testResult.passed = false;
      this.results.failed++;
    }
    
    // Add the test result to the overall results
    this.results.tests.push(testResult);
    this._currentTest = null;
  }
  
  /**
   * Helper to assert a condition
   * @private
   */
  _assert(message, condition, actual) {
    const assertion = {
      message,
      passed: !!condition,
      actual: actual
    };
    
    if (this._currentTest) {
      this._currentTest.assertions.push(assertion);
      
      if (!assertion.passed) {
        this._currentTest.passed = false;
        console.log(`  ❌ ${message}`);
        console.log('  Actual:', actual);
      } else {
        console.log(`  ✅ ${message}`);
      }
    }
    
    return assertion.passed;
  }
}

// Export the test suite
export default GritsClientTests;
