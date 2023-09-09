package proxy

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"grits/internal/grits"
)

func TestAddFileAndReadFromCache(t *testing.T) {
	cacheDir, err := ioutil.TempDir("", "cache-")
	require.NoError(t, err)
	//defer os.RemoveAll(cacheDir)

	config := &Config{
		StorageDirectory: cacheDir,
		StorageSize:      5000000, // 5MB
	}

	fileCache := NewFileCache(config)

	testFileContent := "Hello, World!"
	testFilePath := filepath.Join(cacheDir, "testFile")
	err = ioutil.WriteFile(testFilePath, []byte(testFileContent), 0644)
	require.NoError(t, err)

	writtenFile, err := fileCache.AddLocalFile(testFilePath)
	require.NoError(t, err)

	cachedFile, err := fileCache.ReadFile(writtenFile.Address)
	require.NoError(t, err)
	require.NotNil(t, cachedFile)

	data, err := ioutil.ReadFile(cachedFile.Path)
	require.NoError(t, err)
	require.Equal(t, testFileContent, string(data))
}

func TestAddFilesToOverflowTheCache(t *testing.T) {
	cacheDir, err := ioutil.TempDir("", "cache-")
	require.NoError(t, err)
	//defer os.RemoveAll(cacheDir)

	config := &Config{
		StorageDirectory: cacheDir,
		StorageSize:      1000000, // 1MB
	}

	fileCache := NewFileCache(config)

	numberOfFiles := 20
	fileSize := 100000 // 100kB
	var fileAddresses []*grits.FileAddr

	for i := 0; i < numberOfFiles; i++ {
		testFileContent := fmt.Sprintf("%02d", i) + string(make([]byte, fileSize-2))
		testFilePath := filepath.Join(cacheDir, fmt.Sprintf("testFile%d", i))
		err := ioutil.WriteFile(testFilePath, []byte(testFileContent), 0644)
		require.NoError(t, err)

		writtenFile, err := fileCache.AddLocalFile(testFilePath)
		require.NoError(t, err)
		fileAddresses = append(fileAddresses, writtenFile.Address)
		fileCache.Release(writtenFile)
		
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 8; i++ {
		cachedFile, err := fileCache.ReadFile(fileAddresses[i])
		require.Error(t, err)
		require.Nil(t, cachedFile)
	}

	for i := 12; i < numberOfFiles; i++ {
		cachedFile, err := fileCache.ReadFile(fileAddresses[i])
		require.NoError(t, err)
		require.NotNil(t, cachedFile)
		
		data, err := ioutil.ReadFile(cachedFile.Path)
		require.NoError(t, err)

		expectedContent := fmt.Sprintf("%02d", i) + string(make([]byte, fileSize-2))
		require.Equal(t, expectedContent, string(data))

		fileCache.Release(cachedFile)
	}
}
