package ziputil

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnzipFile(t *testing.T) {
	// Create a temporary directory for test files
	sourceDir, err := os.MkdirTemp("", "zip-source-*")
	require.NoError(t, err)
	defer os.RemoveAll(sourceDir)

	// Create test files
	testFiles := map[string]string{
		"app.py":            "print('Hello, World!')",
		"requirements.txt":  "requests==2.26.0",
		"utils/helpers.py":  "def greet(): return 'Hello'",
		"utils/__init__.py": "",
		"data/sample.json":  "{\"key\": \"value\"}",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(sourceDir, path)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create a zip file from the source directory
	zipFilePath := filepath.Join(sourceDir, "test.zip")
	zipFile, err := os.Create(zipFilePath)
	require.NoError(t, err)
	defer zipFile.Close()

	// Write zip content
	zipContent, err := ZipDir(sourceDir)
	require.NoError(t, err)
	zipFile.Close() // Close to flush content

	// Create a new zipFile from the content for testing
	tempZipFile, err := os.CreateTemp("", "test-*.zip")
	require.NoError(t, err)
	defer os.Remove(tempZipFile.Name())
	defer tempZipFile.Close()

	_, err = tempZipFile.Write(zipContent)
	require.NoError(t, err)
	tempZipFile.Close()

	// Create destination directory for unzipping
	destDir, err := os.MkdirTemp("", "zip-dest-*")
	require.NoError(t, err)
	defer os.RemoveAll(destDir)

	// Test the UnzipFile function
	err = Unzip(tempZipFile.Name(), destDir)
	require.NoError(t, err)

	// Verify that all files exist in the extracted directory
	for path, expectedContent := range testFiles {
		extractedPath := filepath.Join(destDir, path)
		require.FileExists(t, extractedPath)

		content, err := os.ReadFile(extractedPath)
		require.NoError(t, err)
		assert.Equal(t, expectedContent, string(content))
	}

	// Test handling of malicious zip entries (directory traversal)
	maliciousZipPath := filepath.Join(sourceDir, "malicious.zip")
	createMaliciousZip(t, maliciousZipPath)

	// Create destination directory for malicious unzipping
	maliciousDestDir, err := os.MkdirTemp("", "zip-malicious-*")
	require.NoError(t, err)
	defer os.RemoveAll(maliciousDestDir)

	// This should fail with an illegal file path error
	err = Unzip(maliciousZipPath, maliciousDestDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "illegal file path")
}

// createMaliciousZip creates a ZIP file with a path traversal attempt
func createMaliciousZip(t *testing.T, zipPath string) {
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Create a file with path traversal attempt
	traversalPath := "../../../../../etc/passwd"
	fileHeader := &zip.FileHeader{
		Name:   traversalPath,
		Method: zip.Deflate,
	}

	writer, err := zipWriter.CreateHeader(fileHeader)
	require.NoError(t, err)

	_, err = writer.Write([]byte("malicious content"))
	require.NoError(t, err)

	err = zipWriter.Close()
	require.NoError(t, err)
}
