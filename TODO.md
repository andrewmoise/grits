Current todo list:

* Remote mounts of a volume on a different server
* Save and restore config, instead of using a config file
* Security for endpoints, only allow certain characters in main name / identifier things

Backlog:

* Switch to mmap instead of stream file I/O for blobs
* Chunking of files
* Useful file metadata (owner, file mode, timestamp), make "type" not numeric
* Chunked directories
* fsync()
* Directories show "size" as the number of files underneath them
* Health check, do quick diagnostics to make sure things can work (via an endpoint ideally)
* List of semi-urgent refactorings to FUSE mount module
* Switch to YAML for configuration
* Undo history in the volumes
* Better organization of blobs (not all in one directory), better handling of tiny blobs
* Debounce multiple writes into a single consolidated change to the tree
* Performance - e.g. `gatsby new ecommerce-demo https://github.com/gatsbyjs/gatsby-starter-shopify`
* File permissions, defaulting to something safe (in particular read-only for the HTTP endpoints)
* Do a rough security audit of the whole codebase
* HTTP3
* Figure out something for octal modes in metadata
* Switch over client freshness check to whole directory, and make timestamp handling better so we don't reload always on server restart
* Clean up Volume API
* Unify path normalization in GritsClient and SW
* Make hash verifications in client configurable
* Rate limiting and some level of DDOS or malicious request resistance
* Graceful reloading of config file
* Terminal "control panel" showing pertinent information and letting you issue commands
* Make validation of blobs / reference counts when you load, instead of leaking references and thus blobs
* We could communicate client config information (e.g. the list of mirrors) via the volume storage itself
* Fix infinite loop in GritsClient.js, of trying to fetch when the server goes down
* Fix duplication of size limits and cleanup between BlobStore and BlobCache
* Make templating of the JS exports cleaner
* Mandate some limits on what characters are allowed in things (volumes, peerNames, etc)
* Need to worry about key expiration for the certbot keys
* Standardize on URL format for identifying peer and mirror protocol/hostname/port
* Clean up naming, capitalization, consistency issues
* Get rid of NameStore.Link()
* Transition some names from what used to be the "new" approach, to just be the normal name (e.g.
  LinkByMetadata() -> Link()).
* Make mount module able to mount a subdirectory of a volume
* mount module needs automated testing
* Switch some durations to use time.Duration in config files and etc
* Get rid of FileNode.Address()
* Make testbed keep track of errors and indicate success or failure more accurately
* Fix testbed, doesn't seem like it passes when started from clean
* Make HTTP API more best practices (particularly how it returns errors)