/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package z

import (
	"fmt"
)

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


// Allocate explicitly allocates the given size for our mmapped file. For non-fallocate builds
// this thin provisions via truncate. 
 func (m *MmapFile) Allocate(maxSz int64) error {
	return m.Truncate(maxSz)
}