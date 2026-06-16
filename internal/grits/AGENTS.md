package grits
// This is the core library — all the interfaces and implementations for
// content-addressed storage that the server daemon and CLI tools build on.
//
// Key types defined here:
//   BlobAddr     — SHA-256 hash as IPFS CID v0 string (type alias for string)
//   BlobStore    — interface for reading/writing blobs by hash
//   CachedFile   — reference-counted blob handle with Read/Release/Take
//   NameStore    — Merkle-tree namespace mapping paths to content hashes
//   FileNode     — a file or directory in the tree (TreeNode / BlobNode)
//   GNodeMetadata — metadata for a file node (type, size, contentHash, mode, timestamp)
//   Config       — server configuration struct
//
// Files overview:
//   structures.go    — BlobAddr, TypedFileAddr, BlobStore/CachedFile interfaces,
//                      FileNode/TreeNode/BlobNode types, GNodeMetadata
//   blobstore.go     — LocalBlobStore: on-disk blob storage with ref counting,
//                      eviction, fetcher registry, and file locking (~713 lines)
//   blobcache.go     — BlobCache: time-based LRU read-through cache wrapping
//                      a LocalBlobStore, with in-flight request coalescing (~249 lines)
//   namestore.go     — NameStore: path-based namespace on top of the blob store,
//                      with Lookup, Link, MultiLink (atomic multi-path ops),
//                      reference management, and FileTreeWatcher (~2459 lines)
//   config.go        — Config struct, JSON loading, config path construction (~226 lines)
//   debug.go         — Debug flag variables and DebugLog helper (~76 lines)
//
// If you observe the project to be out of sync with any AGENTS.md files,
// or if there is a module that doesn't have enough explanation to quickly
// get the lay of the land, feel free to update (sparingly!) to add more
// explanations or update them. As a general guideline, any source directory
// (or batch of source directories) that has more than 10 source files
// probably needs its own AGENTS.md file.
