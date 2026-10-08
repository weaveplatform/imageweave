// Package delivery connects Packer output to OCI packaging and runtime acceptance.
// Publication remains a separate, authenticated workflow boundary.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

var (
	ErrInput       = errors.New("invalid delivery input")
	commitPattern  = regexp.MustCompile(`^[a-f0-9]{40}$`)
	versionPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$`)
)

// Options binds the recipe claim and vendor source to a new local candidate.
type Options struct {
	Request      plan.Request
	RecipeCommit string
	SourceURI    string
	Version      string
	Out          string
	Packer       string
	OCI          string
	Imageweave   string
}

// Result points at the exact validated layout and its unsigned acceptance report.
type Result struct {
	Layout        string `json:"layout"`
	Acceptance    string `json:"acceptance"`
	Manifest      string `json:"manifest"`
	RecipeCommit  string `json:"recipeCommit"`
	Qualification string `json:"qualification"`
}

// Runner executes an argument vector without a shell and streams diagnostics.
type Runner func(context.Context, string, []string, io.Writer) error

// Exec is the production runner; commands inherit cancellation and log streams.
func Exec(ctx context.Context, binary string, args []string, log io.Writer) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute %s: %w", binary, err)
	}
	return nil
}

// Build runs Packer, imports its output and tests the packed OCI artifact. The
// caller must authenticate source checksums before supplying their pinned hash.
func Build(ctx context.Context, o Options, run Runner, log io.Writer) (Result, error) {
	var result Result
	if err := validate(o, run); err != nil {
		return result, err
	}
	resolved, vars, err := prepare(ctx, o, log)
	if err != nil {
		return result, err
	}
	manifest := filepath.Join(o.Out, "packer", "manifest.json")
	bundle, candidate := filepath.Join(o.Out, "bundle"), filepath.Join(o.Out, "validated")
	commands := []command{
		{o.Packer, []string{"init", resolved.Template}},
		{o.Packer, []string{"validate", "-var-file=" + vars, resolved.Template}},
		{o.Packer, []string{"build", "-color=false", "-var-file=" + vars, resolved.Template}},
		{
			o.OCI,
			[]string{
				"bundle",
				"import-imageweave",
				manifest,
				"--recipe-commit",
				o.RecipeCommit,
				"--source-uri",
				o.SourceURI,
				"--version",
				o.Version,
				"--out",
				bundle,
			},
		},
		{
			o.Imageweave,
			[]string{
				"image",
				"validate-linux",
				bundle,
				"--arches",
				o.Request.Arch,
				"--out",
				candidate,
				"--timeout",
				"1200",
			},
		},
	}
	if err := execute(ctx, commands, run, log); err != nil {
		return result, err
	}
	result = Result{
		Layout:        filepath.Join(candidate, "layout"),
		Acceptance:    filepath.Join(candidate, "acceptance.json"),
		Manifest:      manifest,
		RecipeCommit:  o.RecipeCommit,
		Qualification: "local-acceptance",
	}
	return result, nil
}

func validate(o Options, run Runner) error {
	if err := validateCommon(o, run); err != nil {
		return err
	}
	if o.Imageweave == "" || (o.Request.Family != "ubuntu" && o.Request.Family != "fedora") {
		return fmt.Errorf(
			"%w: Linux base delivery requires Ubuntu/Fedora and an acceptance executable",
			ErrInput,
		)
	}
	return nil
}

func validateCommon(o Options, run Runner) error {
	u, err := url.Parse(o.SourceURI)
	prepared := o.Request.Purpose == "guest-prepared" && o.Request.Parent != nil &&
		o.SourceURI == ""
	if !commitPattern.MatchString(o.RecipeCommit) || !versionPattern.MatchString(o.Version) ||
		(!prepared && (err != nil ||
			u.Scheme != "https" ||
			u.Host == "" ||
			u.User != nil ||
			u.RawQuery != "" ||
			u.Fragment != "")) {
		return fmt.Errorf(
			"%w: commit SHA, immutable version and credential-free HTTPS source URI required",
			ErrInput,
		)
	}
	if !filepath.IsAbs(o.Out) || filepath.Clean(o.Out) != o.Out || run == nil || o.Packer == "" ||
		o.OCI == "" {
		return fmt.Errorf("%w: absolute new output and executable paths required", ErrInput)
	}
	return nil
}

// prepare is shared construction plumbing; qualification is selected by the caller.
func prepare(ctx context.Context, o Options, log io.Writer) (plan.Plan, string, error) {
	o.Request.Workspace = filepath.Join(o.Out, "packer")
	progress := imagebuild.NewNativeProgress(ctx, log, "delivery input verification")
	finish := progress.Start()
	progress.Step("checking pinned source and firmware hashes")
	resolved, err := plan.Resolve(o.Request)
	finish(err)
	if err != nil {
		return plan.Plan{}, "", fmt.Errorf("resolve delivery plan: %w", err)
	}
	if err := os.Mkdir(o.Out, 0o700); err != nil {
		return plan.Plan{}, "", fmt.Errorf("create delivery output: %w", err)
	}
	data, err := json.MarshalIndent(resolved.Variables, "", "  ")
	if err != nil {
		return plan.Plan{}, "", fmt.Errorf("encode Packer variables: %w", err)
	}
	vars := filepath.Join(o.Out, "variables.pkrvars.json")
	if err := os.WriteFile(vars, data, 0o600); err != nil {
		return plan.Plan{}, "", fmt.Errorf("write Packer variables: %w", err)
	}
	return resolved, vars, nil
}

type command struct {
	binary string
	args   []string
}

func execute(ctx context.Context, commands []command, run Runner, log io.Writer) error {
	for _, command := range commands {
		if log != nil {
			_, _ = fmt.Fprintf(
				log,
				"[%s] delivery: %s %s started\n",
				time.Now().UTC().Format(time.RFC3339),
				command.binary,
				command.args[0],
			)
		}
		if err := run(ctx, command.binary, command.args, log); err != nil {
			return fmt.Errorf("delivery %s %s: %w", command.binary, command.args[0], err)
		}
	}
	return nil
}
