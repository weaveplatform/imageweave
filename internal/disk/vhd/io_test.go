package vhd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type faultyDisk struct {
	*os.File
	statErr, readErr, writeErr bool
}

func (f faultyDisk) Stat() (os.FileInfo, error) {
	if f.statErr {
		return nil, io.ErrClosedPipe
	}
	return f.File.Stat()
}

func (f faultyDisk) ReadAt(p []byte, o int64) (int, error) {
	if f.readErr {
		return 0, io.ErrClosedPipe
	}
	return f.File.ReadAt(p, o)
}

func (f faultyDisk) WriteAt(p []byte, o int64) (int, error) {
	if f.writeErr {
		return 0, io.ErrClosedPipe
	}
	return f.File.WriteAt(p, o)
}

func TestFooterIOFailures(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "disk"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(1024); err != nil {
		t.Fatal(err)
	}
	for _, d := range []faultyDisk{{File: f, statErr: true}, {File: f, writeErr: true}} {
		if appendFooter(d, time.Now()) == nil {
			t.Fatal("footer write error ignored")
		}
	}
	if err := appendFooter(f, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, d := range []faultyDisk{{File: f, statErr: true}, {File: f, readErr: true}} {
		if _, err := rawSize(d); err == nil {
			t.Fatal("footer read error ignored")
		}
	}
}
