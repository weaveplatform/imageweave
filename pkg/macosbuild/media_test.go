package macosbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/pkg/buildstorage"
)

func pinnedMedia(data []byte) common.Media {
	hash := sha256.Sum256(data)
	return common.Media{
		URI:    "https://example.test/media.ipsw",
		Size:   int64(len(data)),
		SHA256: hex.EncodeToString(hash[:]),
	}
}

func cacheFile(t *testing.T, path string, data []byte) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(t, os.WriteFile(path, data, 0o600))
}

func TestIPSWReusesPreviousAttempt(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			root := t.TempDir()
			o := options(t)
			o.Workspace = filepath.Join(root, "27-base-r2-1")
			o.SharedMediaCache = shared
			data := []byte("verified Apple media")
			m := pinnedMedia(data)
			old := filepath.Join(root, "27-base-r1-1", "media", m.SHA256+".ipsw")
			cacheFile(t, old, data)
			var log bytes.Buffer
			admissions := 0
			fetch := func(context.Context, common.Media, string) (string, error) {
				t.Fatal("verified IPSW redownloaded")
				return "", nil
			}
			admit := func(_ context.Context, _ Options, remaining uint64, _ io.Writer) error {
				admissions++
				if remaining != 0 {
					t.Fatal("cached media charged twice", remaining)
				}
				return nil
			}
			path, err := downloadIPSW(t.Context(), o, m, &log, admit, fetch)
			must(t, err)
			original, err := os.Stat(old)
			must(t, err)
			cached, err := os.Stat(path)
			must(t, err)
			if !os.SameFile(original, cached) ||
				!strings.Contains(log.String(), "download skipped") {
				t.Fatal("IPSW copied or reuse not logged", log.String())
			}
			// A subsequent attempt must survive deletion of the original failed job.
			must(t, os.RemoveAll(filepath.Dir(filepath.Dir(old))))
			if shared {
				o.Workspace = filepath.Join(root, "27-base-r3-1")
			}
			again, err := downloadIPSW(t.Context(), o, m, nil, admit, fetch)
			must(t, err)
			if again != path || admissions != 2 {
				t.Fatal(again, path, admissions)
			}
		})
	}
}

func TestIPSWDownloadAndResume(t *testing.T) {
	for _, mode := range []string{"fresh", "wrong size", "wrong hash", "other version", "partial", "complete partial"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("the expected IPSW bytes")
			m := pinnedMedia(data)
			requests := 0
			offset := 0
			router := chi.NewRouter()
			router.Get("/media.ipsw", func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-%d", offset, len(data)-1) {
					t.Error("unexpected transfer", r.Header.Get("Range"))
				}
				w.Header().
					Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(data)-1, len(data)))
				w.Header().Set("Content-Length", fmt.Sprint(len(data)-offset))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data[offset:])
			})
			server := httptest.NewServer(router)
			defer server.Close()
			m.URI = server.URL + "/media.ipsw"
			o := options(t)
			dest := filepath.Join(ipswCache(o), m.SHA256+".ipsw")
			switch mode {
			case "wrong size":
				cacheFile(t, dest, []byte("short"))
			case "wrong hash":
				cacheFile(t, dest, bytes.Repeat([]byte{'x'}, len(data)))
			case "other version":
				cacheFile(
					t,
					filepath.Join(ipswCache(o), "other.ipsw"),
					bytes.Repeat([]byte{'z'}, len(data)),
				)
			case "partial", "complete partial":
				offset = len(data) / 2
				if mode == "complete partial" {
					offset = len(data)
				}
				cacheFile(t, dest+".part", data[:offset])
				must(t, common.WriteJSON(dest+".part.json", m))
			}
			var log bytes.Buffer
			admitted := false
			path, err := downloadIPSW(
				t.Context(),
				o,
				m,
				&log,
				func(_ context.Context, _ Options, remaining uint64, _ io.Writer) error {
					admitted = true
					if remaining != uint64(len(data)-offset) {
						t.Fatal("incorrect remaining allocation", remaining)
					}
					return nil
				},
				(common.Downloader{Client: server.Client()}).Download,
			)
			must(t, err)
			must(t, common.VerifiedFile(path, m))
			wantRequests := 1
			if mode == "complete partial" {
				wantRequests = 0
			}
			if !admitted || requests != wantRequests {
				t.Fatal(admitted, requests)
			}
		})
	}
}

func TestIPSWFailurePaths(t *testing.T) {
	for _, mode := range []string{"size", "oversized", "hash", "cancelled", "parent file", "cache file", "cache loop", "cache symlink", "capacity reused", "capacity fresh", "adopt mkdir", "adopt link", "partial metadata", "partial mismatch", "partial oversized", "partial directory", "partial stat", "fetch failed"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			o := options(t)
			o.Workspace = filepath.Join(root, "new")
			o.SharedMediaCache = true
			data := []byte("valid ipsw")
			m := pinnedMedia(data)
			dest := filepath.Join(ipswCache(o), m.SHA256+".ipsw")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "size":
				m.Size = 0
			case "oversized":
				m.Size = int64(41 * buildstorage.GiB)
			case "hash":
				m.SHA256 = "../../injected"
			case "cancelled":
				cancel()
			case "parent file":
				cacheFile(t, filepath.Join(root, "file"), nil)
				o.Workspace = filepath.Join(root, "file", "job")
			case "cache file":
				cacheFile(t, ipswCache(o), nil)
			case "cache loop":
				if runtime.GOOS == "windows" {
					t.Skip("symlink permissions")
				}
				must(t, os.Symlink(ipswCache(o), ipswCache(o)))
			case "cache symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink permissions")
				}
				must(t, os.MkdirAll(ipswCache(o), 0o700))
				must(t, os.Symlink(filepath.Join(root, "missing"), dest))
			case "capacity reused", "adopt mkdir", "adopt link":
				cacheFile(t, filepath.Join(root, "old", "media", m.SHA256+".ipsw"), data)
			case "partial metadata":
				cacheFile(t, dest+".part", data[:2])
			case "partial mismatch":
				cacheFile(t, dest+".part", data[:2])
				other := m
				other.URI += "different"
				must(t, common.WriteJSON(dest+".part.json", other))
			case "partial oversized":
				cacheFile(t, dest+".part", append(data, 'x'))
				must(t, common.WriteJSON(dest+".part.json", m))
			case "partial directory":
				must(t, os.MkdirAll(dest+".part", 0o700))
			case "partial stat":
				if runtime.GOOS == "windows" {
					t.Skip("symlink permissions")
				}
				must(t, os.MkdirAll(ipswCache(o), 0o700))
				must(t, os.Symlink(dest+".part", dest+".part"))
			}
			fetchCalled := false
			_, err := downloadIPSW(
				ctx,
				o,
				m,
				io.Discard,
				func(context.Context, Options, uint64, io.Writer) error {
					switch mode {
					case "capacity fresh", "capacity reused":
						return testFailure
					case "adopt mkdir":
						cacheFile(t, ipswCache(o), nil)
					case "adopt link":
						must(t, os.RemoveAll(filepath.Join(root, "old")))
					}
					return nil
				},
				func(context.Context, common.Media, string) (string, error) {
					fetchCalled = true
					return "", testFailure
				},
			)
			if err == nil {
				t.Fatal("failure hidden")
			}
			if fetchCalled != (mode == "fetch failed") {
				t.Fatal("unexpected network operation", mode, err)
			}
			if strings.HasPrefix(mode, "capacity") && !errors.Is(err, testFailure) {
				t.Fatal(err)
			}
		})
	}
}

type cancelMediaLog struct{ cancel context.CancelFunc }

func (w cancelMediaLog) Write(b []byte) (int, error) { w.cancel(); return len(b), nil }

func TestCacheVerificationCancellation(t *testing.T) {
	o := options(t)
	m := pinnedMedia([]byte("valid ipsw"))
	cacheFile(t, filepath.Join(ipswCache(o), m.SHA256+".ipsw"), []byte("valid ipsw"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := downloadIPSW(
		ctx,
		o,
		m,
		cancelMediaLog{cancel},
		func(context.Context, Options, uint64, io.Writer) error {
			t.Fatal("cancelled verification admitted")
			return nil
		},
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestValidNamedIPSWReplacesCorruptCache(t *testing.T) {
	o := options(t)
	data := []byte("valid ipsw")
	m := pinnedMedia(data)
	cacheFile(t, filepath.Join(ipswCache(o), "0-Apple-Restore.ipsw"), data)
	cacheFile(
		t,
		filepath.Join(ipswCache(o), m.SHA256+".ipsw"),
		bytes.Repeat([]byte{'x'}, len(data)),
	)
	_, err := downloadIPSW(
		t.Context(),
		o,
		m,
		nil,
		func(context.Context, Options, uint64, io.Writer) error { return nil },
		func(context.Context, common.Media, string) (string, error) {
			t.Fatal("valid media redownloaded")
			return "", nil
		},
	)
	must(t, err)
}
