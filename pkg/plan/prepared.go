package plan

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	ocidigest "github.com/opencontainers/go-digest"

	"github.com/weaveplatform/imageweave/pkg/catalog"
)

func resolvePrepared(r Request) (Plan, error) {
	s, err := catalog.Current().Resolve(r.Family, r.Release, r.Arch)
	if err != nil {
		return Plan{}, err
	}
	template, err := s.Template(r.Purpose, r.Target)
	if err != nil {
		return Plan{}, err
	}
	p := r.Parent
	if p == nil || r.SchemaVersion != 1 || p.Name == "" || p.Ref == "" ||
		ocidigest.Digest(p.Digest).Validate() != nil ||
		!strings.HasPrefix(p.Digest, "sha256:") ||
		r.SourceBuild == "" {
		return Plan{}, fmt.Errorf(
			"%w: prepared image requires schema 1 and pinned parent",
			ErrInput,
		)
	}
	if r.SourceURL != "" || r.SourceSHA256 != "" || r.SSHPrivateKey != "" || r.SSHPublicKey != "" {
		return Plan{}, fmt.Errorf(
			"%w: prepared input is a parent image; media and external SSH keys are not accepted",
			ErrInput,
		)
	}
	for _, path := range []string{p.Layout, r.Workspace} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
			strings.ContainsAny(path, "\r\n\x00") {
			return Plan{}, fmt.Errorf(
				"%w: clean absolute parent layout and workspace required",
				ErrInput,
			)
		}
	}
	if r.Timeout == "" {
		r.Timeout = "45m"
	}
	d, err := time.ParseDuration(r.Timeout)
	if err != nil || d <= 0 {
		return Plan{}, fmt.Errorf("%w: positive prepared timeout required", ErrInput)
	}
	return Plan{
		SchemaVersion: 1,
		Selection:     s,
		Template:      template,
		Qualification: "unverified",
		Variables: map[string]any{
			"parent_layout":    p.Layout,
			"parent_ref":       p.Ref,
			"parent_name":      p.Name,
			"parent_digest":    p.Digest,
			"source_build":     r.SourceBuild,
			"output_directory": filepath.Join(r.Workspace, "candidate"),
			"timeout":          r.Timeout,
		},
	}, nil
}
