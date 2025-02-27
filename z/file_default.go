//go:build !linux && !fallocate
// +build !linux,!fallocate

/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package z

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
)

func OpenMmapFileUsing(fd *os.File, sz int, writable bool) (*MmapFile, error) {
	filename := fd.Name()
	fi, err := fd.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "cannot stat file: %s", filename)
	}

	var rerr error
	fileSize := fi.Size()
	if sz > 0 && fileSize == 0 {
		// If file is empty, truncate it to sz.
		if err := fd.Truncate(int64(sz)); err != nil {
			return nil, errors.Wrapf(err, "error while truncation")
		}
		fileSize = int64(sz)
		rerr = NewFile
	}

	// fmt.Printf("Mmaping file: %s with writable: %v filesize: %d\n", fd.Name(), writable, fileSize)
	buf, err := Mmap(fd, writable, fileSize) // Mmap up to file size.
	if err != nil {
		return nil, errors.Wrapf(err, "while mmapping %s with size: %d", fd.Name(), fileSize)
	}

	if fileSize == 0 {
		dir, _ := filepath.Split(filename)
		if err := SyncDir(dir); err != nil {
			return nil, err
		}
	}
	return &MmapFile{
		Data: buf,
		Fd:   fd,
		mut:  sync.RWMutex{},
	}, rerr
}

// Truncate would truncate the mmapped file to the given size. On Linux, we truncate
// the underlying file and then call mremap, but on other systems, we unmap first,
// then truncate, then re-map.
func (m *MmapFile) Truncate(maxSz int64) error {
	if err := m.Sync(); err != nil {
		return fmt.Errorf("while sync file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := Munmap(m.Data); err != nil {
		return fmt.Errorf("while munmap file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := m.Fd.Truncate(maxSz); err != nil {
		return fmt.Errorf("while truncate file: %s, error: %v\n", m.Fd.Name(), err)
	}
	var err error
	m.Data, err = Mmap(m.Fd, true, maxSz) // Mmap up to max size.
	return err
}

// Truncate would truncate the mmapped file to the given size. On Linux, we truncate
// the underlying file and then call mremap, but on other systems, we unmap first,
// then truncate, then re-map.
func (m *MmapFile) Allocate(maxSz int64) error {
	return m.Truncate(maxSz)
}
