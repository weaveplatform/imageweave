package macosbuild

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/buildstorage"
)

func TestStoragePlanning(t *testing.T) {
	for _, mode := range []string{"auto", "explicit", "default scratch", "all", "invalid", "bad scratch", "discovery failed", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			o := options(t)
			o.Tier = "base"
			o.ScratchRoot = t.TempDir()
			o.Revision = "1"
			o.Workspace = "auto"
			if mode == "explicit" {
				o.Workspace = t.TempDir()
			}
			if mode == "default scratch" {
				o.ScratchRoot = ""
			}
			if mode == "all" {
				o.Release = "all"
				o.Tier = "all"
				o.Revision = ""
			}
			if mode == "invalid" {
				o.Release = "99"
			}
			if mode == "bad scratch" {
				o.ScratchRoot = "relative"
			}
			var log bytes.Buffer
			p, e := planStorage(
				t.Context(),
				o,
				&log,
				func(_ context.Context, host, workspace string) (buildstorage.Inventory, error) {
					if mode == "discovery failed" {
						return buildstorage.Inventory{}, testFailure
					}
					free := uint64(2000) * buildstorage.GiB
					if mode == "capacity" {
						free = 1
					}
					root := workspace
					if workspace == "auto" {
						root = t.TempDir()
					}
					return buildstorage.Inventory{
						Host: buildstorage.Volume{
							Root:       host,
							Filesystem: "apfs",
							Pools:      []string{"host"},
						},
						Volumes: []buildstorage.Volume{
							{Root: root, Filesystem: "apfs", Pools: []string{"images"}},
						},
						Pools: map[string]uint64{"host": 100 * buildstorage.GiB, "images": free},
					}, nil
				},
			)
			success := mode == "auto" || mode == "explicit" || mode == "default scratch" ||
				mode == "all"
			if (e == nil) != success {
				t.Fatal(p, e)
			}
			if success && (p.Workspace == "" || log.Len() == 0 || !p.Serial) {
				t.Fatal(p)
			}
		})
	}
}

func TestStoredWorkflow(t *testing.T) {
	for _, mode := range []string{"ok", "invalid", "plan failed", "scratch failed", "build failed"} {
		t.Run(mode, func(t *testing.T) {
			o := options(t)
			if mode == "invalid" {
				o.Timeout = "bad"
			}
			scratch := filepath.Join(t.TempDir(), "scratch")
			if mode == "scratch failed" {
				must(t, os.WriteFile(scratch, nil, 0o600))
			}
			called := false
			_, e := storedWorkflow(
				t.Context(),
				o,
				nil,
				func(context.Context, Options, io.Writer) (buildstorage.Plan, error) {
					if mode == "plan failed" {
						return buildstorage.Plan{}, testFailure
					}
					return buildstorage.Plan{Workspace: o.Workspace, Scratch: scratch}, nil
				},
				func(_ context.Context, selected Options, _ io.Writer) ([]Result, error) {
					called = true
					if selected.ScratchRoot != scratch {
						t.Fatal(selected)
					}
					if mode == "build failed" {
						return nil, testFailure
					}
					return nil, nil
				},
			)
			if (e == nil) != (mode == "ok") {
				t.Fatal(e)
			}
			if called != (mode == "ok" || mode == "build failed") {
				t.Fatal("build ran before storage admission")
			}
		})
	}
}

func TestOversizedRestoreMediaRejected(t *testing.T) {
	s := source()
	s.Size = int64(41 * buildstorage.GiB)
	_, _, e := buildTier(
		t.Context(),
		options(t),
		s,
		"base",
		nil,
		tools{commit: testCommit},
		services{},
		nil,
	)
	if !errors.Is(e, ErrBuild) {
		t.Fatal(e)
	}
}

func TestStorageCacheUnavailable(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LocalAppData", "")
	o := options(t)
	_, err := PlanStorage(t.Context(), o, nil)
	if err == nil {
		t.Fatal("missing user cache accepted")
	}
}

func TestSharedScratchSerializesWorkspaces(t *testing.T) {
	scratch := t.TempDir()
	o := options(t)
	planner := func(_ context.Context, o Options, _ io.Writer) (buildstorage.Plan, error) {
		return buildstorage.Plan{Workspace: o.Workspace, Scratch: scratch}, nil
	}
	_, err := storedWorkflow(
		t.Context(),
		o,
		nil,
		planner,
		func(context.Context, Options, io.Writer) ([]Result, error) {
			other := options(t)
			_, nestedErr := storedWorkflow(
				t.Context(),
				other,
				nil,
				planner,
				func(context.Context, Options, io.Writer) ([]Result, error) {
					t.Fatal("overlapping images admitted")
					return nil, nil
				},
			)
			if nestedErr == nil {
				t.Fatal("shared scratch lock ignored")
			}
			return nil, testFailure
		},
	)
	if !errors.Is(err, testFailure) {
		t.Fatal(err)
	}
	_, err = storedWorkflow(
		t.Context(),
		o,
		nil,
		planner,
		func(context.Context, Options, io.Writer) ([]Result, error) { return nil, nil },
	)
	if err != nil {
		t.Fatal("lock not released after failure:", err)
	}
}

func TestStageCapacityDoesNotReserveDownloadedMediaAgain(t *testing.T) {
	o := options(t)
	o.Tier = "base"
	o.ScratchRoot = t.TempDir()
	o.SharedMediaCache = true
	free := uint64(367)*buildstorage.GiB + buildstorage.GiB/2
	inventory := func(context.Context, string, string) (buildstorage.Inventory, error) {
		return buildstorage.Inventory{
			Host: buildstorage.Volume{
				Root:       o.ScratchRoot,
				Filesystem: "apfs",
				Pools:      []string{"host"},
			},
			Volumes: []buildstorage.Volume{
				{Root: o.Workspace, Filesystem: "apfs", Pools: []string{"images"}},
			},
			Pools: map[string]uint64{"host": 20 * buildstorage.GiB, "images": free},
		}, nil
	}
	// This exact capacity failed after the successful 26,637,307,067-byte transfer.
	p, err := planStorageStage(t.Context(), o, nil, 0, inventory)
	must(t, err)
	if p.Budget.Image != 320*buildstorage.GiB ||
		p.MediaCache != filepath.Join(filepath.Dir(o.Workspace), "media") {
		t.Fatal(p)
	}
	remaining := uint64(26637307067)
	free = 328*buildstorage.GiB + remaining - 1
	if _, err = planStorageStage(
		t.Context(),
		o,
		nil,
		remaining,
		inventory,
	); !errors.Is(
		err,
		buildstorage.ErrCapacity,
	) {
		t.Fatal("download admitted without remaining capacity", err)
	}
	if _, err = planStorageStage(t.Context(), o, nil, 41*buildstorage.GiB, inventory); err == nil {
		t.Fatal("oversized media admitted")
	}
}

func TestAutomaticWorkflowEnablesSharedCache(t *testing.T) {
	o := options(t)
	o.Workspace = "auto"
	chosen := t.TempDir()
	scratch := t.TempDir()
	_, err := storedWorkflow(
		t.Context(),
		o,
		nil,
		func(context.Context, Options, io.Writer) (buildstorage.Plan, error) {
			return buildstorage.Plan{Workspace: chosen, Scratch: scratch}, nil
		},
		func(_ context.Context, selected Options, _ io.Writer) ([]Result, error) {
			if !selected.SharedMediaCache || selected.Workspace != chosen {
				t.Fatal(selected)
			}
			return nil, nil
		},
	)
	must(t, err)
}
