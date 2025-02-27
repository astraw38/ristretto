//go:build linux && fallocate
// +build linux,fallocate

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
	"syscall"

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
		// Allocate
		if err := syscall.Fallocate(int(fd.Fd()), 0, 0, int64(sz)); err != nil {
			// if we return nil here, we leak an FD & don't release the space, release
			// our resources so upstream can recover how they want.
			fd.Close()
			os.Remove(filename)
			return nil, fmt.Errorf("while fallocate file: %s, error: %v", fd.Name(), err)
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

// Allocate explicitly allocates the given size for our mmapped file
func (m *MmapFile) Allocate(maxSz int64) error {
	if err := m.Sync(); err != nil {
		return fmt.Errorf("while sync file: %s, error: %v\n", m.Fd.Name(), err)
	}

	i, err := m.Fd.Stat()
	if err != nil {
		return fmt.Errorf("while stat file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := syscall.Fallocate(int(m.Fd.Fd()), 0, i.Size(), maxSz); err != nil {
		return fmt.Errorf("while fallocate file: %s, error: %v\n", m.Fd.Name(), err)
	}

	m.Data, err = mremap(m.Data, int(maxSz)) // Mmap up to max size.
	return err
}

// Truncate would truncate the mmapped file to the given size. On Linux, we truncate
// the underlying file and then call mremap, but on other systems, we unmap first,
// then truncate, then re-map.
func (m *MmapFile) Truncate(maxSz int64) error {
	if err := m.Sync(); err != nil {
		return fmt.Errorf("while sync file: %s, error: %v\n", m.Fd.Name(), err)
	}
	if err := m.Fd.Truncate(maxSz); err != nil {
		return fmt.Errorf("while truncate file: %s, error: %v\n", m.Fd.Name(), err)
	}

	var err error
	m.Data, err = mremap(m.Data, int(maxSz)) // Mmap up to max size.
	return err
}
