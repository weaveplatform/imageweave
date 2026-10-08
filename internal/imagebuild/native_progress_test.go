package imagebuild

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNativeProgress(t *testing.T) {
	var log bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	p := NewNativeProgress(ctx, &log, "restore macos/arm64")
	p.Step("loading image")
	_, err := p.Write([]byte("guest progress\n"))
	must(t, err)
	if !strings.Contains(log.String(), "guest progress\n") {
		t.Fatal("guest output buffered")
	}
	ticks, stop, done := make(chan time.Time), make(chan struct{}), make(chan struct{})
	go func() { defer close(done); p.watch(ticks, stop) }()
	ticks <- p.started.Add(2 * time.Minute)
	close(stop)
	<-done
	for _, want := range []string{"elapsed=2m0s remaining=0s", "serial-bytes=15", "last-serial="} {
		if !strings.Contains(log.String(), want) {
			t.Fatal("missing heartbeat detail", want, log.String())
		}
	}
	finish := p.Start()
	finish(nil)
	if !strings.Contains(log.String(), "completed") {
		t.Fatal(log.String())
	}
	q := NewNativeProgress(t.Context(), &log, "install windows/amd64")
	q.Step("waiting for Setup")
	q.Start()(context.Canceled)
	if !strings.Contains(log.String(), "remaining=no deadline") ||
		!strings.Contains(log.String(), "failed: context canceled") {
		t.Fatal(log.String())
	}
	q.out = brokenProgressLog{}
	if _, err := q.Write([]byte("log")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	NewNativeProgress(t.Context(), nil, "silent").Step("running")
}

type brokenProgressLog struct{}

func (brokenProgressLog) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
