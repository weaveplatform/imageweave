package nativebuild

import (
	"context"
	"io"
)

// MacRestoreRequest is independent of any guestweave VM configuration or storage.
type MacRestoreRequest struct {
	IPSW, Directory string
	DiskSize        int64
	Log             io.Writer
}

// MacRestoreResult contains only the platform requirements carried by an OCI image.
type MacRestoreResult struct {
	HardwareModel string `json:"HardwareModel"`
	CPUCountMin   int64  `json:"CPUCountMin"`
	CPUCount      int64  `json:"CPUCount"`
	MemorySizeMin int64  `json:"MemorySizeMin"`
	MemorySize    int64  `json:"MemorySize"`
}

// MacRestoreFunc is the native Apple restore boundary, injectable for orchestration tests.
type MacRestoreFunc func(context.Context, MacRestoreRequest) (MacRestoreResult, error)
