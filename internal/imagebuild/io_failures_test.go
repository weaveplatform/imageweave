package imagebuild

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

func TestDownloadFilesystemFailures(t *testing.T) {
	for _, mode := range []string{"cache-loop", "parent-file", "partial-loop", "metadata-directory", "partial-directory", "promotion", "metadata-removal"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "image")
			data := []byte("verified media")
			m := mediaFor("https://example.test/image", data)
			switch mode {
			case "cache-loop":
				must(t, os.Symlink(out, out))
			case "parent-file":
				must(t, os.WriteFile(out, nil, 0o600))
				out = filepath.Join(out, "image")
			case "partial-loop":
				must(t, os.Symlink(out+".part", out+".part"))
			case "metadata-directory":
				must(t, os.Mkdir(out+".part.json", 0o700))
			case "partial-directory":
				must(t, os.Mkdir(out+".part", 0o700))
				must(t, WriteJSON(out+".part.json", m))
			}
			client := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					switch mode {
					case "promotion":
						must(t, os.Mkdir(out, 0o700))
					case "metadata-removal":
						must(t, os.Remove(out+".part.json"))
						must(t, os.Mkdir(out+".part.json", 0o700))
						must(t, os.WriteFile(filepath.Join(out+".part.json", "keep"), nil, 0o600))
					}
					return &http.Response{
						StatusCode:    200,
						Body:          io.NopCloser(bytes.NewReader(data)),
						ContentLength: int64(len(data)),
						Header:        http.Header{},
						Request:       r,
					}, nil
				}),
			}
			if _, err := (Downloader{Client: client}).Download(t.Context(), m, out); err == nil {
				t.Fatal("ignored filesystem failure")
			}
			if mode != "metadata-removal" && mode != "parent-file" {
				info, err := os.Stat(out)
				if err == nil && info.Mode().IsRegular() {
					t.Fatal("promoted failed download")
				}
			}
		})
	}
}

func TestDownloadClosedHandles(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "partial"))
	must(t, err)
	must(t, f.Close())
	m := mediaFor("https://example.test/image", []byte("x"))
	if err := (Downloader{}).transfer(t.Context(), f, m); err == nil {
		t.Fatal("closed file accepted")
	}
	for _, status := range []int{200, 206} {
		client := &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    status,
					Header:        http.Header{"Content-Range": []string{"bytes 0-0/1"}},
					ContentLength: 1,
					Body:          io.NopCloser(strings.NewReader("x")),
					Request:       r,
				}, nil
			}),
		}
		if err := (Downloader{}).downloadRange(t.Context(), client, f, m, 0); err == nil {
			t.Fatal("closed destination accepted")
		}
	}
	dir := t.TempDir()
	stat, err := os.Stat(dir)
	must(t, err)
	if err := VerifiedFile(dir, Media{Size: stat.Size()}); err == nil {
		t.Fatal("directory hashed as media")
	}
}

func TestCandidateRejectsInvalidLayouts(t *testing.T) {
	for _, mode := range []string{"invalid-index", "no-tag", "missing-blob"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			store, err := pack.OpenLayout(t.Context(), dir)
			must(t, err)
			if mode == "invalid-index" {
				must(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte("{"), 0o600))
			}
			if mode == "missing-blob" {
				raw := []byte(`{"schemaVersion":2,"manifests":[]}`)
				desc := ocispec.Descriptor{
					MediaType: ocispec.MediaTypeImageIndex,
					Digest:    digest.FromBytes(raw),
					Size:      int64(len(raw)),
				}
				must(t, store.Push(t.Context(), desc, bytes.NewReader(raw)))
				must(t, store.Tag(t.Context(), desc, "test"))
				must(t, os.Remove(filepath.Join(dir, "blobs", "sha256", desc.Digest.Encoded())))
			}
			if _, _, err := OpenCandidate(t.Context(), dir); err == nil {
				t.Fatal("accepted " + mode)
			}
		})
	}
}

func TestDeepCheckRejectsIncompleteIndex(t *testing.T) {
	store, err := pack.OpenLayout(t.Context(), t.TempDir())
	must(t, err)
	raw := []byte(
		`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`,
	)
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}
	must(t, store.Push(t.Context(), desc, bytes.NewReader(raw)))
	if report, err := DeepCheck(t.Context(), store, desc); err == nil || report.OK() {
		t.Fatal("empty candidate accepted", err)
	}
}
