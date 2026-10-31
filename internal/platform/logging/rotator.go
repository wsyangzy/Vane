package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileRotator manages log file rotation and archival
type FileRotator struct {
	filename    string // Current log filename
	archiveRoot string
	stream      string
	currentDay  string
	maxSize     int64 // Maximum size in bytes before rotation
	maxFiles    int   // Maximum number of rotated files to keep
	compress    bool  // Whether to compress rotated files
	currentFile *os.File
	currentSize int64
	mutex       sync.Mutex
}

// NewFileRotator creates a new file rotator
func NewFileRotator(filename string, maxSize int64, maxFiles int, compress bool) (*FileRotator, error) {
	fr := &FileRotator{
		filename: filename,
		maxSize:  maxSize,
		maxFiles: maxFiles,
		compress: compress,
	}

	// Open initial file
	if err := fr.openFile(); err != nil {
		return nil, err
	}

	return fr, nil
}

// NewArchivingFileRotator keeps the active log at filename and archives
// completed daily or size-rotated files under environment/stream/month.
func NewArchivingFileRotator(filename, rootDir, environment, stream string, maxSize int64, maxFiles int, compress bool) (*FileRotator, error) {
	fr := &FileRotator{
		filename:    filename,
		archiveRoot: filepath.Join(rootDir, environment, stream),
		stream:      stream,
		maxSize:     maxSize,
		maxFiles:    maxFiles,
		compress:    compress,
	}
	if err := fr.openFile(); err != nil {
		return nil, err
	}
	return fr, nil
}

// Write implements io.Writer interface
func (fr *FileRotator) Write(p []byte) (int, error) {
	return fr.writeAt(p, time.Now())
}

func (fr *FileRotator) writeAt(p []byte, now time.Time) (int, error) {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()

	day := now.Format("2006-01-02")
	if fr.archiveRoot != "" {
		if fr.currentSize > 0 && fr.currentDay != "" && fr.currentDay != day {
			if err := fr.rotate(now); err != nil {
				return 0, fmt.Errorf("failed to archive daily log file: %w", err)
			}
		}
		fr.currentDay = day
	}

	// Check if we need to rotate
	if fr.maxSize > 0 && fr.currentSize > 0 && fr.currentSize+int64(len(p)) > fr.maxSize {
		if err := fr.rotate(now); err != nil {
			return 0, fmt.Errorf("failed to rotate log file: %w", err)
		}
	}

	// Write to current file
	n, err := fr.currentFile.Write(p)
	if err != nil {
		return n, err
	}

	fr.currentSize += int64(n)
	return n, nil
}

// Close closes the current log file
func (fr *FileRotator) Close() error {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()

	if fr.currentFile != nil {
		return fr.currentFile.Close()
	}
	return nil
}

// Sync syncs the current log file to disk
func (fr *FileRotator) Sync() error {
	fr.mutex.Lock()
	defer fr.mutex.Unlock()

	if fr.currentFile != nil {
		return fr.currentFile.Sync()
	}
	return nil
}

// openFile opens or creates the log file
func (fr *FileRotator) openFile() error {
	if err := os.MkdirAll(filepath.Dir(fr.filename), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(fr.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get current file size
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to get log file info: %w", err)
	}

	fr.currentFile = file
	fr.currentSize = info.Size()
	if fr.archiveRoot != "" && fr.currentSize > 0 && fr.currentDay == "" {
		fr.currentDay = info.ModTime().Format("2006-01-02")
	}
	return nil
}

// rotate rotates the current log file
func (fr *FileRotator) rotate(now time.Time) error {
	// Close current file
	if fr.currentFile != nil {
		if err := fr.currentFile.Close(); err != nil {
			return err
		}
		fr.currentFile = nil
	}

	rotatedName := fr.filename + "." + now.Format("2006-01-02-15-04-05.000000000")
	if fr.archiveRoot != "" {
		day := fr.currentDay
		if _, err := time.Parse("2006-01-02", day); err != nil {
			day = now.Format("2006-01-02")
		}
		archiveDir := filepath.Join(fr.archiveRoot, day[:7])
		if err := os.MkdirAll(archiveDir, 0755); err != nil {
			return fmt.Errorf("failed to create log archive directory: %w", err)
		}
		rotatedName = filepath.Join(archiveDir, fmt.Sprintf("%s-%s.log", fr.stream, day))
		if fileExists(rotatedName) || fileExists(rotatedName+".gz") {
			rotatedName = filepath.Join(archiveDir, fmt.Sprintf("%s-%s.%s.log", fr.stream, day, now.Format("15-04-05.000000000")))
		}
	}

	if err := os.Rename(fr.filename, rotatedName); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	if fr.compress {
		go func() {
			fr.compressFile(rotatedName)
			fr.cleanupRotatedFiles()
		}()
	} else {
		go fr.cleanupRotatedFiles()
	}

	// Open new file
	return fr.openFile()
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// compressFile compresses a rotated log file
func (fr *FileRotator) compressFile(filename string) {
	src, err := os.Open(filename)
	if err != nil {
		return
	}
	defer src.Close()

	dst, err := os.Create(filename + ".gz")
	if err != nil {
		return
	}

	gw := gzip.NewWriter(dst)
	if _, err := io.Copy(gw, src); err != nil {
		_ = gw.Close()
		_ = dst.Close()
		return
	}
	if err := gw.Close(); err != nil {
		_ = dst.Close()
		return
	}
	if err := dst.Close(); err != nil {
		return
	}

	_ = os.Remove(filename)
}

// cleanupOldFiles removes old rotated files beyond maxFiles limit
func (fr *FileRotator) cleanupOldFiles(filename string) {
	if fr.maxFiles <= 0 {
		return
	}

	dir := filepath.Dir(filename)
	base := filepath.Base(filename)

	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Collect rotated files
	var rotatedFiles []os.DirEntry
	for _, file := range files {
		if strings.HasPrefix(file.Name(), base+".") {
			rotatedFiles = append(rotatedFiles, file)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(rotatedFiles, func(i, j int) bool {
		infoI, errI := rotatedFiles[i].Info()
		infoJ, errJ := rotatedFiles[j].Info()
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Remove files beyond maxFiles limit
	for i := fr.maxFiles; i < len(rotatedFiles); i++ {
		os.Remove(filepath.Join(dir, rotatedFiles[i].Name()))
	}
}

func (fr *FileRotator) cleanupRotatedFiles() {
	if fr.archiveRoot == "" {
		fr.cleanupOldFiles(fr.filename)
		return
	}
	if fr.maxFiles <= 0 {
		return
	}

	var rotatedFiles []string
	_ = filepath.Walk(fr.archiveRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".log") || strings.HasSuffix(info.Name(), ".log.gz") {
			rotatedFiles = append(rotatedFiles, path)
		}
		return nil
	})
	sort.Slice(rotatedFiles, func(i, j int) bool {
		infoI, errI := os.Stat(rotatedFiles[i])
		infoJ, errJ := os.Stat(rotatedFiles[j])
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})
	for i := fr.maxFiles; i < len(rotatedFiles); i++ {
		_ = os.Remove(rotatedFiles[i])
	}
}

// ParseSize parses size string like "100MB", "1GB" into bytes
func ParseSize(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(strings.ToUpper(sizeStr))

	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		numStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "B") {
		multiplier = 1
		numStr = strings.TrimSuffix(sizeStr, "B")
	} else {
		// No suffix, assume bytes
		numStr = sizeStr
	}

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}
	if num <= 0 || num > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("size must be positive and fit in int64: %s", sizeStr)
	}

	return num * multiplier, nil
}
