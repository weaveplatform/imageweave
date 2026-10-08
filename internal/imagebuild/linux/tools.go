// Package linux builds and qualifies Linux base, agent and desktop candidates.
package linux

import (
	"context"
	"fmt"
	"io"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

type (
	Tools    common.Tools
	Packages struct {
		Tools      Tools
		Downloader common.Downloader
	}
	Lock           = common.Lock
	Asset          = common.Asset
	Media          = common.Media
	ModuleInput    = common.ModuleInput
	PlatformInputs = common.PlatformInputs
	AgentOptions   = common.AgentOptions
)

func (t Tools) run(ctx context.Context, out io.Writer, name string, args ...string) error {
	if err := common.Tools(t).RunCommand(ctx, out, name, args...); err != nil {
		return fmt.Errorf("linux command: %w", err)
	}
	return nil
}

func (t Tools) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	data, err := common.Tools(t).Output(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("linux command output: %w", err)
	}
	return data, nil
}
func (t Tools) progress(format string, args ...any) { common.Tools(t).Progress(format, args...) }

func (p Packages) PrepareLinux(
	ctx context.Context,
	l Lock,
	arch, cache, out string,
) ([]Asset, error) {
	sources, err := (common.Packages{Tools: common.Tools(p.Tools), Downloader: p.Downloader}).PrepareLinux(
		ctx,
		l,
		arch,
		cache,
		out,
	)
	if err != nil {
		return nil, fmt.Errorf("prepare Linux packages: %w", err)
	}
	return sources, nil
}
