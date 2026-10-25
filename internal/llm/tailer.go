package llm

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

// accessEntry is the subset of a Caddy JSON access log line the daemon needs.
type accessEntry struct {
	TS       float64 `json:"ts"`
	Duration float64 `json:"duration"`
	Request  struct {
		RemoteIP string `json:"remote_ip"`
		URI      string `json:"uri"`
	} `json:"request"`
}

// tailer follows an access log file and yields parsed entries. It detects log
// rotation by comparing the inode and reopens the file from the start.
type tailer struct {
	path string

	file   *os.File
	reader *bufio.Reader
	inode  uint64
	offset int64

	// resumeInode and resumeOffset describe the position persisted by the
	// previous run. When they match the file, reading continues from there so
	// a restart never counts the same requests twice.
	resumeInode  uint64
	resumeOffset int64
	initialized  bool
}

// newTailer creates a tailer for the given path.
func newTailer(path string) *tailer {
	return &tailer{path: path}
}

// resume configures the position persisted by the previous run.
func (t *tailer) resume(inode uint64, offset int64) {
	t.resumeInode = inode
	t.resumeOffset = offset
}

// open opens the log file and remembers its inode. It returns false when the
// file does not exist yet.
func (t *tailer) open() (bool, error) {
	if _, err := os.Stat(t.path); err != nil {
		// A missing log file is not an error: the proxy has not written
		// anything yet, or has not started. Report "nothing to read" instead
		// so the monitor stays in the waiting state.
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}

	file, err := os.Open(t.path)
	if err != nil {
		return false, err
	}
	// Stat the handle, not the path: the file may have been replaced between
	// the stat above and the open, and a mismatched inode would replay it.
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return false, err
	}

	start := int64(0)
	if !t.initialized {
		t.initialized = true
		// Continue where the previous run stopped. A different inode means the
		// log was rotated while the daemon was down, so the new file is read
		// from the beginning: it holds traffic that was never counted.
		if t.resumeInode != 0 && fileInode(opened) == t.resumeInode &&
			t.resumeOffset >= 0 && t.resumeOffset <= opened.Size() {
			start = t.resumeOffset
		}
	}

	if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			file.Close()
			return false, err
		}
	}

	t.file = file
	t.reader = bufio.NewReaderSize(file, 64*1024)
	t.inode = fileInode(opened)
	t.offset = start
	return true, nil
}

// position returns the inode and byte offset of the next unread byte.
func (t *tailer) position() (uint64, int64) {
	return t.inode, t.offset
}

// fileInode returns the inode number of a stat result, or 0 when unavailable.
func fileInode(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}

// read returns all complete entries appended since the last call. A rotated
// file is detected and reopened from the beginning.
func (t *tailer) read() ([]accessEntry, error) {
	if t.file == nil {
		opened, err := t.open()
		if err != nil {
			return nil, err
		}
		if !opened {
			return nil, nil
		}
	}

	var entries []accessEntry

	// Detect rotation and in-place truncation.
	info, statErr := os.Stat(t.path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return entries, statErr
	}
	if statErr == nil {
		ino := fileInode(info)
		switch {
		case ino != 0 && t.inode != 0 && ino != t.inode:
			// The old file may have received lines since the last poll, so
			// read them before switching to the new inode.
			drained, err := t.drain()
			entries = append(entries, drained...)
			if err != nil {
				return entries, err
			}
			if _, err := t.open(); err != nil {
				return entries, err
			}
		case info.Size() < t.offset:
			// Truncated in place, as copytruncate does: start over.
			if err := t.restart(); err != nil {
				return entries, err
			}
		}
	}

	for {
		line, err := t.reader.ReadString('\n')
		if len(line) > 0 && err == nil {
			// Complete line: process it.
			if entry, ok := parseAccessEntry(line); ok {
				entries = append(entries, entry)
			}
			t.offset += int64(len(line))
			continue
		}
		if err == io.EOF {
			if len(line) > 0 {
				// Partial line: seek back to the last complete line so the
				// fragment is read again once the writer finishes it.
				if err := t.rewind(); err != nil {
					return entries, err
				}
			}
			return entries, nil
		}
		if err != nil {
			return entries, err
		}
	}
}

// drain reads the remaining complete lines from the current file.
func (t *tailer) drain() ([]accessEntry, error) {
	var entries []accessEntry
	if t.file == nil {
		return entries, nil
	}
	for {
		line, err := t.reader.ReadString('\n')
		if len(line) > 0 && err == nil {
			if entry, ok := parseAccessEntry(line); ok {
				entries = append(entries, entry)
			}
			t.offset += int64(len(line))
			continue
		}
		if err == io.EOF {
			return entries, nil
		}
		return entries, err
	}
}

// restart seeks back to the beginning after the file was truncated in place.
func (t *tailer) restart() error {
	if t.file == nil {
		return nil
	}
	if _, err := t.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	t.reader = bufio.NewReaderSize(t.file, 64*1024)
	t.offset = 0
	return nil
}

// rewind seeks back to the last known complete-line offset. It is used when
// the last line of the file is incomplete.
func (t *tailer) rewind() error {
	if t.file == nil {
		return nil
	}
	if _, err := t.file.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	t.reader = bufio.NewReaderSize(t.file, 64*1024)
	return nil
}

// parseAccessEntry parses a single Caddy JSON access log line.
func parseAccessEntry(line string) (accessEntry, bool) {
	var entry accessEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return entry, false
	}
	if entry.Request.RemoteIP == "" {
		return entry, false
	}
	return entry, true
}

// dayKey returns the UTC day key for a timestamp.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
