package nativebuild

import (
	"io"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type brokenBootLog struct{}

func (brokenBootLog) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func windowsFixture() (WindowsSelection, WindowsSource) {
	return WindowsSelection{
		"pro-26h2",
		"amd64",
		"en-US",
	}, WindowsSource{
		Release:  "26H2",
		Edition:  "pro",
		Arch:     "amd64",
		Language: "en-US",
	}
}
