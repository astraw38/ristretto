/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package z

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/pkg/errors"
)

// MmapFile represents an mmapd file and includes both the buffer to the data
// and the file descriptor.
type MmapFile struct {
	Data []byte
	Fd   *os.File
	mut  sync.RWMutex
}

// When run with thick provisioning, we cannot rely on
// the filedescriptor size to determine the real size of our
// mmapped file.
func (m *MmapFile) SetSize(s uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()
	binary.BigEndian.PutUint32(m.Data, s)
}

func (m *MmapFile) GetSize() uint32 {
	m.mut.RLock()
	defer m.mut.RUnlock()
	return binary.BigEndian.Uint32(m.Data)
}

func (m *MmapFile) IncrementSizeBy(s uint32) {
	m.mut.Lock()
	defer m.mut.Unlock()
	existing := binary.BigEndian.Uint32(m.Data)
	binary.BigEndian.PutUint32(m.Data, s+existing)
}

var NewFile = errors.New("Create a new file")

// OpenMmapFile opens an existing file or creates a new file. If the file is
// created, it would truncate the file to maxSz. In both cases, it would mmap
// the file to maxSz and returned it. In case the file is created, z.NewFile is
// returned.
func OpenMmapFile(filename string, flag int, maxSz int) (*MmapFile, error) {
	// fmt.Printf("opening file %s with flag: %v\n", filename, flag)
	fd, err := os.OpenFile(filename, flag, 0666)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open: %s", filename)
	}
	writable := true
	if flag == os.O_RDONLY {
		writable = false
	}
	return OpenMmapFileUsing(fd, maxSz, writable)
}

type mmapReader struct {
	Data   []byte
	offset int
}

func (mr *mmapReader) Read(buf []byte) (int, error) {
	if mr.offset > len(mr.Data) {
		return 0, io.EOF
	}
	n := copy(buf, mr.Data[mr.offset:])
	mr.offset += n
	if n < len(buf) {
		return n, io.EOF
	}
	return n, nil
}

func (m *MmapFile) NewReader(offset int) io.Reader {
	return &mmapReader{
		Data:   m.Data,
		offset: offset,
	}
}

// Bytes returns data starting from offset off of size sz. If there's not enough data, it would
// return nil slice and io.EOF.
func (m *MmapFile) Bytes(off, sz int) ([]byte, error) {
	if len(m.Data[off:]) < sz {
		return nil, io.EOF
	}
	return m.Data[off : off+sz], nil
}

// Slice returns the slice at the given offset.
func (m *MmapFile) Slice(offset int) []byte {
	sz := binary.BigEndian.Uint32(m.Data[offset:])
	start := offset + 4
	next := start + int(sz)
	if next > len(m.Data) {
		return []byte{}
	}
	res := m.Data[start:next]
	return res
}

// AllocateSlice allocates a slice of the given size at the given offset.
func (m *MmapFile) AllocateSlice(sz, offset int) ([]byte, int, error) {
	start := offset + 4

	// If the file is too small, double its size or increase it by 1GB, whichever is smaller.
	if start+sz > len(m.Data) {
		const oneGB = 1 << 30
		growBy := len(m.Data)
		if growBy > oneGB {
			growBy = oneGB
		}
		if growBy < sz+4 {
			growBy = sz + 4
		}
		if err := m.Allocate(int64(len(m.Data) + growBy)); err != nil {
			return nil, 0, err
		}
	}

	binary.BigEndian.PutUint32(m.Data[offset:], uint32(sz))
	return m.Data[start : start+sz], start + sz, nil
}

func (m *MmapFile) Sync() error {
	if m == nil {
		return nil
	}
	return Msync(m.Data)
}

func (m *MmapFile) Delete() error {
	// Badger can set the m.Data directly, without setting any Fd. In that case, this should be a
	// NOOP.
	if m.Fd == nil {
		return nil
	}

	if err := Munmap(m.Data); err != nil {
		return fmt.Errorf("while munmap file: %s, error: %v\n", m.Fd.Name(), err)
	}
	m.Data = nil
	if err := m.Fd.Truncate(0); err != nil {
		return fmt.Errorf("while truncate file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := m.Fd.Close(); err != nil {
		return fmt.Errorf("while close file: %s, error: %v\n", m.Fd.Name(), err)
	}
	return os.Remove(m.Fd.Name())
}

// Close would close the file. It would also truncate the file if maxSz >= 0.
func (m *MmapFile) Close(maxSz int64) error {
	// Badger can set the m.Data directly, without setting any Fd. In that case, this should be a
	// NOOP.
	if m.Fd == nil {
		return nil
	}
	if err := m.Sync(); err != nil {
		return fmt.Errorf("while sync file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := Munmap(m.Data); err != nil {
		return fmt.Errorf("while munmap file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if maxSz >= 0 {
		if err := m.Fd.Truncate(maxSz); err != nil {
			return fmt.Errorf("while truncate file: %s, error: %v\n", m.Fd.Name(), err)
		}
	}
	return m.Fd.Close()
}

func SyncDir(dir string) error {
	df, err := os.Open(dir)
	if err != nil {
		return errors.Wrapf(err, "while opening %s", dir)
	}
	if err := df.Sync(); err != nil {
		return errors.Wrapf(err, "while syncing %s", dir)
	}
	if err := df.Close(); err != nil {
		return errors.Wrapf(err, "while closing %s", dir)
	}
	return nil
}
