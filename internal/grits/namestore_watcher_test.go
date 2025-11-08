package grits

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// MockWatcher implements FileTreeWatcher for testing
type MockWatcher struct {
	notifications []WatcherNotification
	mtx           sync.Mutex
}

type WatcherNotification struct {
	Path     string
	OldValue FileNode
	NewValue FileNode
}

func (mw *MockWatcher) OnFileTreeChange(path string, oldValue FileNode, newValue FileNode) error {
	mw.mtx.Lock()
	defer mw.mtx.Unlock()

	mw.notifications = append(mw.notifications, WatcherNotification{
		Path:     path,
		OldValue: oldValue,
		NewValue: newValue,
	})
	return nil
}

func (mw *MockWatcher) GetNotifications() []WatcherNotification {
	mw.mtx.Lock()
	defer mw.mtx.Unlock()

	result := make([]WatcherNotification, len(mw.notifications))
	copy(result, mw.notifications)
	return result
}

func (mw *MockWatcher) Clear() {
	mw.mtx.Lock()
	defer mw.mtx.Unlock()

	mw.notifications = nil
}

func (mw *MockWatcher) WaitForNotification(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mw.mtx.Lock()
		count := len(mw.notifications)
		mw.mtx.Unlock()

		if count > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestWatcherBasicNotification tests that watchers receive notifications for direct changes
func TestWatcherBasicNotification(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create a watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Create content
	content := []byte("test content")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	// Link a file - should trigger notification
	err = ns.linkBlob("testfile.txt", blob.GetAddress(), blob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob: %v", err)
	}

	// Check we got a notification
	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	notif := notifications[0]
	if notif.Path != "testfile.txt" {
		t.Errorf("Expected path 'testfile.txt', got '%s'", notif.Path)
	}

	if notif.OldValue != nil {
		t.Errorf("Expected OldValue to be nil (file didn't exist), got %v", notif.OldValue)
	}

	if notif.NewValue == nil {
		t.Errorf("Expected NewValue to be non-nil")
	}
}

// TestWatcherUpdateNotification tests notifications when updating an existing file
func TestWatcherUpdateNotification(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create initial content
	content1 := []byte("content 1")
	blob1, err := bs.AddDataBlock(content1)
	if err != nil {
		t.Fatalf("Failed to add blob1: %v", err)
	}
	defer blob1.Release()

	// Link initial file
	err = ns.linkBlob("updatetest.txt", blob1.GetAddress(), blob1.GetSize())
	if err != nil {
		t.Fatalf("Failed to link initial blob: %v", err)
	}

	// Now register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Create updated content
	content2 := []byte("content 2")
	blob2, err := bs.AddDataBlock(content2)
	if err != nil {
		t.Fatalf("Failed to add blob2: %v", err)
	}
	defer blob2.Release()

	// Update the file - should trigger notification
	err = ns.linkBlob("updatetest.txt", blob2.GetAddress(), blob2.GetSize())
	if err != nil {
		t.Fatalf("Failed to update blob: %v", err)
	}

	// Check notification
	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	notif := notifications[0]
	if notif.Path != "updatetest.txt" {
		t.Errorf("Expected path 'updatetest.txt', got '%s'", notif.Path)
	}

	if notif.OldValue == nil {
		t.Errorf("Expected OldValue to be non-nil (file existed)")
	}

	if notif.NewValue == nil {
		t.Errorf("Expected NewValue to be non-nil")
	}

	// Verify the old and new values are different
	if notif.OldValue != nil && notif.NewValue != nil {
		if notif.OldValue.MetadataBlob().GetAddress() == notif.NewValue.MetadataBlob().GetAddress() {
			t.Errorf("Expected OldValue and NewValue to be different")
		}
	}
}

// TestWatcherDeleteNotification tests notifications when deleting a file
func TestWatcherDeleteNotification(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create content
	content := []byte("to be deleted")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	// Link file
	err = ns.linkBlob("deleteme.txt", blob.GetAddress(), blob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob: %v", err)
	}

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Delete the file
	err = ns.Link("deleteme.txt", nil)
	if err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// Check notification
	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	notif := notifications[0]
	if notif.Path != "deleteme.txt" {
		t.Errorf("Expected path 'deleteme.txt', got '%s'", notif.Path)
	}

	if notif.OldValue == nil {
		t.Errorf("Expected OldValue to be non-nil (file existed)")
	}

	if notif.NewValue != nil {
		t.Errorf("Expected NewValue to be nil (file deleted), got %v", notif.NewValue)
	}
}

// TestWatcherNoNotificationForParentChange tests that changes to parent directories
// don't trigger notifications (only the specific path that changed gets notified)
func TestWatcherNoNotificationForParentChange(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create directory structure
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	err = ns.linkTree("parent", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create parent dir: %v", err)
	}

	err = ns.linkTree("parent/child", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create child dir: %v", err)
	}

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Add a file deep in the tree
	content := []byte("deep file")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	err = ns.linkBlob("parent/child/file.txt", blob.GetAddress(), blob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob: %v", err)
	}

	// Check notifications - we should only get one for the actual file path
	notifications := watcher.GetNotifications()

	// We expect exactly 1 notification for "parent/child/file.txt"
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	// Verify it's for the right path
	if notifications[0].Path != "parent/child/file.txt" {
		t.Errorf("Expected notification for 'parent/child/file.txt', got '%s'", notifications[0].Path)
	}

	// Make sure we didn't get notifications for parent paths
	for _, notif := range notifications {
		if notif.Path == "parent" || notif.Path == "parent/child" || notif.Path == "" {
			t.Errorf("Got unexpected notification for parent path: %s", notif.Path)
		}
	}
}

// TestWatcherNoNotificationForSiblingChange tests that changes to sibling paths
// don't trigger notifications
func TestWatcherNoNotificationForSiblingChange(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create directory structure
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	err = ns.linkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Add first file
	content1 := []byte("file 1")
	blob1, err := bs.AddDataBlock(content1)
	if err != nil {
		t.Fatalf("Failed to add blob1: %v", err)
	}
	defer blob1.Release()

	err = ns.linkBlob("dir/file1.txt", blob1.GetAddress(), blob1.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob1: %v", err)
	}

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Add sibling file
	content2 := []byte("file 2")
	blob2, err := bs.AddDataBlock(content2)
	if err != nil {
		t.Fatalf("Failed to add blob2: %v", err)
	}
	defer blob2.Release()

	err = ns.linkBlob("dir/file2.txt", blob2.GetAddress(), blob2.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob2: %v", err)
	}

	// Check notifications
	notifications := watcher.GetNotifications()

	// Should only get notification for file2.txt, not file1.txt
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	if notifications[0].Path != "dir/file2.txt" {
		t.Errorf("Expected notification for 'dir/file2.txt', got '%s'", notifications[0].Path)
	}
}

// TestWatcherMultipleWatchers tests that multiple watchers all receive notifications
func TestWatcherMultipleWatchers(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Register multiple watchers
	watcher1 := &MockWatcher{}
	watcher2 := &MockWatcher{}
	watcher3 := &MockWatcher{}

	ns.RegisterWatcher(watcher1)
	ns.RegisterWatcher(watcher2)
	ns.RegisterWatcher(watcher3)

	defer ns.UnregisterWatcher(watcher1)
	defer ns.UnregisterWatcher(watcher2)
	defer ns.UnregisterWatcher(watcher3)

	// Create and link a file
	content := []byte("test content")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	err = ns.linkBlob("multiwatch.txt", blob.GetAddress(), blob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob: %v", err)
	}

	// All watchers should have received the notification
	for i, watcher := range []*MockWatcher{watcher1, watcher2, watcher3} {
		notifications := watcher.GetNotifications()
		if len(notifications) != 1 {
			t.Errorf("Watcher %d: expected 1 notification, got %d", i+1, len(notifications))
		}

		if len(notifications) > 0 && notifications[0].Path != "multiwatch.txt" {
			t.Errorf("Watcher %d: expected path 'multiwatch.txt', got '%s'", i+1, notifications[0].Path)
		}
	}
}

// TestWatcherUnregister tests that unregistered watchers don't receive notifications
func TestWatcherUnregister(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)

	// Create and link first file
	content1 := []byte("content 1")
	blob1, err := bs.AddDataBlock(content1)
	if err != nil {
		t.Fatalf("Failed to add blob1: %v", err)
	}
	defer blob1.Release()

	err = ns.linkBlob("file1.txt", blob1.GetAddress(), blob1.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob1: %v", err)
	}

	// Should have one notification
	if len(watcher.GetNotifications()) != 1 {
		t.Fatalf("Expected 1 notification before unregister, got %d", len(watcher.GetNotifications()))
	}

	// Unregister watcher
	ns.UnregisterWatcher(watcher)
	watcher.Clear()

	// Create and link second file
	content2 := []byte("content 2")
	blob2, err := bs.AddDataBlock(content2)
	if err != nil {
		t.Fatalf("Failed to add blob2: %v", err)
	}
	defer blob2.Release()

	err = ns.linkBlob("file2.txt", blob2.GetAddress(), blob2.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob2: %v", err)
	}

	// Should have no new notifications
	if len(watcher.GetNotifications()) != 0 {
		t.Errorf("Expected 0 notifications after unregister, got %d", len(watcher.GetNotifications()))
	}
}

// TestWatcherRootChange tests notification when the root itself changes
func TestWatcherRootChange(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Create new root
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	// Replace root
	err = ns.linkTree("", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to replace root: %v", err)
	}

	// Check notification
	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification for root change, got %d", len(notifications))
	}

	if notifications[0].Path != "" {
		t.Errorf("Expected path to be empty string (root), got '%s'", notifications[0].Path)
	}
}

// TestWatcherMultiLinkNotifications tests notifications from MultiLink operations
func TestWatcherMultiLinkNotifications(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Setup directory structure
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	err = ns.linkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Create content blobs
	blob1, err := bs.AddDataBlock([]byte("content 1"))
	if err != nil {
		t.Fatalf("Failed to add blob1: %v", err)
	}
	defer blob1.Release()

	blob2, err := bs.AddDataBlock([]byte("content 2"))
	if err != nil {
		t.Fatalf("Failed to add blob2: %v", err)
	}
	defer blob2.Release()

	// Create metadata for the blobs
	_, metadata1, err := ns.CreateMetadataBlob(blob1.GetAddress(), blob1.GetSize(), false, 0)
	if err != nil {
		t.Fatalf("Failed to create metadata1: %v", err)
	}
	defer metadata1.Release()

	_, metadata2, err := ns.CreateMetadataBlob(blob2.GetAddress(), blob2.GetSize(), false, 0)
	if err != nil {
		t.Fatalf("Failed to create metadata2: %v", err)
	}
	defer metadata2.Release()

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Use MultiLink to add multiple files at once
	requests := []*LinkRequest{
		{
			Path:    "dir/file1.txt",
			NewAddr: metadata1.GetAddress(),
		},
		{
			Path:    "dir/file2.txt",
			NewAddr: metadata2.GetAddress(),
		},
	}

	_, err = ns.MultiLink(requests, false)
	if err != nil {
		t.Fatalf("Failed to MultiLink: %v", err)
	}

	// Check notifications - should get one for each file
	notifications := watcher.GetNotifications()
	if len(notifications) != 2 {
		t.Fatalf("Expected 2 notifications from MultiLink, got %d", len(notifications))
	}

	// Verify we got notifications for both files
	paths := make(map[string]bool)
	for _, notif := range notifications {
		paths[notif.Path] = true
	}

	if !paths["dir/file1.txt"] {
		t.Errorf("Missing notification for dir/file1.txt")
	}
	if !paths["dir/file2.txt"] {
		t.Errorf("Missing notification for dir/file2.txt")
	}
}

// TestWatcherEdgeCaseEmptyPath tests notification for empty path (root)
func TestWatcherEdgeCaseEmptyPath(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Create content
	emptyDirInterface, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	// Try to link at root (empty string path)
	err = ns.linkTree("", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to link at root: %v", err)
	}

	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}

	if notifications[0].Path != "" {
		t.Errorf("Expected empty path (root), got '%s'", notifications[0].Path)
	}
}

// TestWatcherNestedDirectoryDeletion tests notifications when deleting a directory
// with nested content
func TestWatcherNestedDirectoryDeletion(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Setup nested structure
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	err = ns.linkTree("parent", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create parent: %v", err)
	}

	err = ns.linkTree("parent/child", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create child: %v", err)
	}

	content := []byte("nested file")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	err = ns.linkBlob("parent/child/file.txt", blob.GetAddress(), blob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link blob: %v", err)
	}

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Delete the parent directory
	err = ns.Link("parent", nil)
	if err != nil {
		t.Fatalf("Failed to delete parent: %v", err)
	}

	// Should only get notification for "parent", not for children
	notifications := watcher.GetNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification for parent deletion, got %d", len(notifications))
	}

	if notifications[0].Path != "parent" {
		t.Errorf("Expected notification for 'parent', got '%s'", notifications[0].Path)
	}

	// Verify we didn't get notifications for child paths
	for _, notif := range notifications {
		if notif.Path == "parent/child" || notif.Path == "parent/child/file.txt" {
			t.Errorf("Got unexpected notification for child path: %s", notif.Path)
		}
	}
}

// TestWatcherConcurrentNotifications tests that notifications work correctly
// with concurrent operations
func TestWatcherConcurrentNotifications(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Setup directory
	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}
	defer emptyDir.Release()

	err = ns.linkTree("concurrent", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create concurrent dir: %v", err)
	}

	// Register watcher
	watcher := &MockWatcher{}
	ns.RegisterWatcher(watcher)
	defer ns.UnregisterWatcher(watcher)

	// Create content
	content := []byte("concurrent test")
	blob, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add blob: %v", err)
	}
	defer blob.Release()

	// Perform concurrent operations
	var wg sync.WaitGroup
	numOps := 50

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			path := fmt.Sprintf("concurrent/file%d.txt", index)
			err := ns.linkBlob(path, blob.GetAddress(), blob.GetSize())
			if err != nil {
				t.Errorf("Failed to link %s: %v", path, err)
			}
		}(i)
	}

	wg.Wait()

	// Give a moment for all notifications to be processed
	time.Sleep(100 * time.Millisecond)

	// Should have received notifications for all operations
	notifications := watcher.GetNotifications()
	if len(notifications) != numOps {
		t.Errorf("Expected %d notifications, got %d", numOps, len(notifications))
	}

	// Verify all paths are unique and correct
	paths := make(map[string]bool)
	for _, notif := range notifications {
		if paths[notif.Path] {
			t.Errorf("Received duplicate notification for path: %s", notif.Path)
		}
		paths[notif.Path] = true
	}
}
