package activitylog

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStore handles JSON file persistence with rotation.
type FileStore struct {
	basePath    string
	currentFile string
	currentSize int64
	maxSize     int64
	mu          sync.Mutex
	writer      *bufio.Writer
	file        *os.File
}

// NewFileStore creates a new file storage instance.
func NewFileStore(basePath string, maxSize int64) (*FileStore, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base path: %w", err)
	}

	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 100MB default
	}

	fs := &FileStore{
		basePath: basePath,
		maxSize:  maxSize,
	}

	if err := fs.openCurrentFile(); err != nil {
		return nil, err
	}

	return fs, nil
}

// openCurrentFile opens or creates the current log file.
func (fs *FileStore) openCurrentFile() error {
	today := time.Now().Format("2006-01-02")
	fs.currentFile = filepath.Join(fs.basePath, fmt.Sprintf("activity_%s.jsonl", today))

	// Check if file exists and get its size
	info, err := os.Stat(fs.currentFile)
	if err == nil {
		fs.currentSize = info.Size()
	} else {
		fs.currentSize = 0
	}

	file, err := os.OpenFile(fs.currentFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	fs.file = file
	fs.writer = bufio.NewWriter(file)
	return nil
}

// Write appends a log entry to the current file.
func (fs *FileStore) Write(entry LogEntry) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check if rotation needed
	if fs.currentSize >= fs.maxSize {
		if err := fs.rotate(); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	// Serialize entry
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	// Write to file
	if _, err := fs.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write entry: %w", err)
	}
	if _, err := fs.writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush to ensure data is written
	if err := fs.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	fs.currentSize += int64(len(data)) + 1
	return nil
}

// WriteBatch writes multiple entries efficiently.
func (fs *FileStore) WriteBatch(entries []LogEntry) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, entry := range entries {
		// Check rotation before each entry
		if fs.currentSize >= fs.maxSize {
			if err := fs.rotate(); err != nil {
				return fmt.Errorf("failed to rotate file: %w", err)
			}
		}

		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry: %w", err)
		}

		if _, err := fs.writer.Write(data); err != nil {
			return fmt.Errorf("failed to write entry: %w", err)
		}
		if _, err := fs.writer.Write([]byte("\n")); err != nil {
			return fmt.Errorf("failed to write newline: %w", err)
		}

		fs.currentSize += int64(len(data)) + 1
	}

	return fs.writer.Flush()
}

// rotate closes the current file and starts a new one.
func (fs *FileStore) rotate() error {
	// Flush and close current file
	if err := fs.writer.Flush(); err != nil {
		return err
	}
	if err := fs.file.Close(); err != nil {
		return err
	}

	// Compress the old file
	go fs.compressFile(fs.currentFile)

	// Create new file with timestamp
	timestamp := time.Now().Format("20060102_150405")
	fs.currentFile = filepath.Join(fs.basePath, fmt.Sprintf("activity_%s.jsonl", timestamp))
	fs.currentSize = 0

	file, err := os.OpenFile(fs.currentFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new file: %w", err)
	}

	fs.file = file
	fs.writer = bufio.NewWriter(file)
	return nil
}

// compressFile compresses a log file using gzip.
func (fs *FileStore) compressFile(filepath string) {
	// Read original file
	data, err := os.ReadFile(filepath)
	if err != nil {
		return
	}

	// Create compressed file
	gzPath := filepath + ".gz"
	gzFile, err := os.Create(gzPath)
	if err != nil {
		return
	}
	defer gzFile.Close()

	// Compress
	gzWriter := gzip.NewWriter(gzFile)
	defer gzWriter.Close()

	if _, err := gzWriter.Write(data); err != nil {
		return
	}

	// Remove original file
	os.Remove(filepath)
}

// Cleanup removes files older than the retention period.
func (fs *FileStore) Cleanup(retention time.Duration) error {
	cutoff := time.Now().Add(-retention)

	entries, err := os.ReadDir(fs.basePath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(fs.basePath, entry.Name())
			if err := os.Remove(path); err != nil {
				// Log error but continue
				continue
			}
		}
	}

	return nil
}

// Close closes the current file.
func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := fs.writer.Flush(); err != nil {
		return err
	}
	return fs.file.Close()
}

// GetCurrentSize returns the size of the current file.
func (fs *FileStore) GetCurrentSize() int64 {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.currentSize
}

// GetCurrentFile returns the path of the current file.
func (fs *FileStore) GetCurrentFile() string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.currentFile
}
