package forwarder

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// spool is a disk-backed append-only buffer for a single upstream's pending
// OTLP records. Writes are non-blocking; a single goroutine per upstream
// drains the spool and retries failed deliveries with exponential backoff.
//
// On-disk record format:
//
//	[2 bytes] path_len  (uint16 LE)
//	[N bytes] path
//	[2 bytes] ct_len    (uint16 LE)
//	[M bytes] content_type
//	[4 bytes] body_len  (uint32 LE)
//	[K bytes] body
type spool struct {
	mu         sync.Mutex
	path       string
	offsetPath string
	f          *os.File
	readOff    int64 // committed (delivered) byte position
	writeOff   int64 // end-of-file / next write position
	maxBytes   int64
	dropped    atomic.Int64
	notify     chan struct{} // buffered(1): signals a new record was appended
}

// spoolRecord is a decoded record with enough metadata to commit it.
type spoolRecord struct {
	path     string
	ct       string
	body     []byte
	size     int64 // total bytes occupied on disk
	startOff int64 // value of readOff when this record was read (for commit guard)
}

// openSpool opens (or creates) the spool file for an upstream identified by
// urlHash.  The committed read offset is persisted in a companion .offset file
// so it survives restarts.
func openSpool(dir, urlHash string, maxBytes int64) (*spool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, urlHash+".spool")
	offsetPath := filepath.Join(dir, urlHash+".offset")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	writeOff := fi.Size()

	var readOff int64
	if data, err := os.ReadFile(offsetPath); err == nil && len(data) == 8 {
		readOff = int64(binary.LittleEndian.Uint64(data))
	}
	if readOff < 0 || readOff > writeOff {
		readOff = 0
	}

	return &spool{
		path:       path,
		offsetPath: offsetPath,
		f:          f,
		readOff:    readOff,
		writeOff:   writeOff,
		maxBytes:   maxBytes,
		notify:     make(chan struct{}, 1),
	}, nil
}

// write appends a record, enforces the size cap (dropping oldest if needed),
// and signals the send loop.
func (s *spool) write(path, ct string, body []byte) {
	pLen := uint16(len(path))
	cLen := uint16(len(ct))
	bLen := uint32(len(body))

	total := 2 + int(pLen) + 2 + int(cLen) + 4 + int(bLen)
	rec := make([]byte, total)
	n := 0
	binary.LittleEndian.PutUint16(rec[n:], pLen)
	n += 2
	copy(rec[n:], path)
	n += int(pLen)
	binary.LittleEndian.PutUint16(rec[n:], cLen)
	n += 2
	copy(rec[n:], ct)
	n += int(cLen)
	binary.LittleEndian.PutUint32(rec[n:], bLen)
	n += 4
	copy(rec[n:], body)

	s.mu.Lock()
	if _, err := s.f.WriteAt(rec, s.writeOff); err == nil {
		s.writeOff += int64(total)
		s.enforceCap()
	}
	s.mu.Unlock()

	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// readNext returns the next undelivered record without advancing readOff.
// Returns (nil, nil) when the spool is empty.  The caller must commit the
// record after successful delivery.
func (s *spool) readNext() (*spoolRecord, error) {
	s.mu.Lock()
	startOff := s.readOff
	end := s.writeOff
	s.mu.Unlock()

	if startOff >= end {
		return nil, nil
	}

	pos := startOff

	// path
	var h2 [2]byte
	if _, err := s.f.ReadAt(h2[:], pos); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	pLen := int(binary.LittleEndian.Uint16(h2[:]))
	pos += 2
	pathBytes := make([]byte, pLen)
	if pLen > 0 {
		if _, err := s.f.ReadAt(pathBytes, pos); err != nil {
			return nil, err
		}
	}
	pos += int64(pLen)

	// content-type
	if _, err := s.f.ReadAt(h2[:], pos); err != nil {
		return nil, err
	}
	cLen := int(binary.LittleEndian.Uint16(h2[:]))
	pos += 2
	ctBytes := make([]byte, cLen)
	if cLen > 0 {
		if _, err := s.f.ReadAt(ctBytes, pos); err != nil {
			return nil, err
		}
	}
	pos += int64(cLen)

	// body
	var h4 [4]byte
	if _, err := s.f.ReadAt(h4[:], pos); err != nil {
		return nil, err
	}
	bLen := int(binary.LittleEndian.Uint32(h4[:]))
	pos += 4
	bodyBytes := make([]byte, bLen)
	if bLen > 0 {
		if _, err := s.f.ReadAt(bodyBytes, pos); err != nil {
			return nil, err
		}
	}
	pos += int64(bLen)

	return &spoolRecord{
		path:     string(pathBytes),
		ct:       string(ctBytes),
		body:     bodyBytes,
		size:     pos - startOff,
		startOff: startOff,
	}, nil
}

// commit advances the read offset past rec and persists it to disk.
// If the spool becomes empty the file is truncated (compacted).
// A guard on rec.startOff makes this safe if enforceCap already advanced
// readOff past this record while the send was in flight.
func (s *spool) commit(rec *spoolRecord) {
	s.mu.Lock()
	if s.readOff == rec.startOff {
		s.readOff += rec.size
	}
	readOff := s.readOff
	empty := s.readOff >= s.writeOff
	if empty {
		s.compact()
	}
	s.mu.Unlock()

	if !empty {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(readOff))
		_ = os.WriteFile(s.offsetPath, buf[:], 0o600)
	}
}

// compact truncates the file when all records have been delivered.
// Must be called with s.mu held.
func (s *spool) compact() {
	_ = s.f.Truncate(0)
	s.readOff = 0
	s.writeOff = 0
	var zero [8]byte
	_ = os.WriteFile(s.offsetPath, zero[:], 0o600)
}

// pendingBytes returns the number of undelivered bytes.
func (s *spool) pendingBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.writeOff - s.readOff
	if p < 0 {
		return 0
	}
	return p
}

// enforceCap drops the oldest pending records until pending bytes ≤ maxBytes.
// Must be called with s.mu held.
func (s *spool) enforceCap() {
	for s.writeOff-s.readOff > s.maxBytes {
		size, err := s.recordSizeAt(s.readOff)
		if err != nil || size <= 0 {
			break
		}
		s.readOff += size
		s.dropped.Add(1)
	}
}

// recordSizeAt reads just the length headers at off to compute the total byte
// count of the record.  Called from enforceCap (under s.mu); f.ReadAt is safe
// to call under a mutex.
func (s *spool) recordSizeAt(off int64) (int64, error) {
	var h2 [2]byte
	if _, err := s.f.ReadAt(h2[:], off); err != nil {
		return 0, err
	}
	pLen := int64(binary.LittleEndian.Uint16(h2[:]))
	off += 2 + pLen

	if _, err := s.f.ReadAt(h2[:], off); err != nil {
		return 0, err
	}
	cLen := int64(binary.LittleEndian.Uint16(h2[:]))
	off += 2 + cLen

	var h4 [4]byte
	if _, err := s.f.ReadAt(h4[:], off); err != nil {
		return 0, err
	}
	bLen := int64(binary.LittleEndian.Uint32(h4[:]))

	return 2 + pLen + 2 + cLen + 4 + bLen, nil
}
