package buildstorage

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func measure(path string) (uint64, string, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, "", fmt.Errorf("free space: %w", err)
	}
	f, err := os.CreateTemp(path, ".imageweave-storage-probe-")
	if err != nil {
		return 0, "", fmt.Errorf("workspace is not writable: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return s.Bavail * uint64(s.Bsize), unix.ByteSliceToString(s.Mntonname[:]), nil
}
