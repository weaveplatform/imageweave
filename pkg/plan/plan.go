// Package plan validates pinned inputs before handing a scenario to Packer.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/nativebuild"
)

var ErrInput = errors.New("invalid build request")

var digest = regexp.MustCompile("^[a-f0-9]{64}$")

// File pins a local firmware input independently of its filename.
type File struct {
	Path   string `yaml:"path"   json:"path"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// Request contains only build configuration, not SSH private key contents.
type Parent struct {
	Layout string `yaml:"layout" json:"layout"`
	Ref    string `yaml:"ref"    json:"ref"`
	Name   string `yaml:"name"   json:"name"`
	Digest string `yaml:"digest" json:"digest"`
}

type Request struct {
	Parent        *Parent `yaml:"parent,omitempty" json:"parent,omitempty"`
	Edition       string  `yaml:"edition"          json:"edition,omitempty"`
	Language      string  `yaml:"language"         json:"language,omitempty"`
	Timeout       string  `yaml:"timeout"          json:"timeout,omitempty"`
	SchemaVersion int     `yaml:"schemaVersion"    json:"schemaVersion"`
	Family        string  `yaml:"family"           json:"family"`
	Release       string  `yaml:"release"          json:"release"`
	Arch          string  `yaml:"arch"             json:"arch"`
	Purpose       string  `yaml:"purpose"          json:"purpose"`
	Target        string  `yaml:"target"           json:"target"`
	SourceURL     string  `yaml:"sourceURL"        json:"sourceURL"`
	SourceSHA256  string  `yaml:"sourceSHA256"     json:"sourceSHA256"`
	SourceBuild   string  `yaml:"sourceBuild"      json:"sourceBuild"`
	Workspace     string  `yaml:"workspace"        json:"workspace"`
	SSHPrivateKey string  `yaml:"sshPrivateKey"    json:"sshPrivateKey"`
	SSHPublicKey  string  `yaml:"sshPublicKey"     json:"sshPublicKey"`
	FirmwareCode  File    `yaml:"firmwareCode"     json:"firmwareCode"`
	FirmwareVars  File    `yaml:"firmwareVars"     json:"firmwareVars"`
	Accelerator   string  `yaml:"accelerator"      json:"accelerator"`
}

// Plan is a resolved candidate build, never acceptance or publication permission.
type Plan struct {
	SchemaVersion int               `json:"schemaVersion"`
	Selection     catalog.Selection `json:"selection"`
	Template      string            `json:"template"`
	Qualification string            `json:"qualification"`
	Variables     map[string]any    `json:"variables"`
}

// Decode rejects unknown fields, duplicate keys and extra YAML documents.
func Decode(r io.Reader) (Request, error) {
	var req Request
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if err := d.Decode(&req); err != nil {
		return req, fmt.Errorf("%w: decode: %w", ErrInput, err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return req, fmt.Errorf("%w: exactly one YAML document required", ErrInput)
	}
	return req, nil
}

// Resolve verifies local inputs and freezes the catalog selection.
// Source authentication is the caller's responsibility; Packer verifies its byte hash.
func Resolve(r Request) (Plan, error) {
	if r.Purpose == "guest-prepared" {
		return resolvePrepared(r)
	}
	if r.Parent != nil {
		return Plan{}, fmt.Errorf("%w: parent only applies to prepared images", ErrInput)
	}
	if r.Family == "macos" || r.Family == "windows-11" {
		return resolveNative(r)
	}
	if err := validate(r); err != nil {
		return Plan{}, err
	}
	s, err := catalog.Current().Resolve(r.Family, r.Release, r.Arch)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve catalog: %w", err)
	}
	template, err := s.Template(r.Purpose, r.Target)
	if err != nil {
		return Plan{}, fmt.Errorf("select template: %w", err)
	}
	if filepath.IsAbs(r.SourceURL) {
		if err := verifyFile(File{Path: r.SourceURL, SHA256: r.SourceSHA256}); err != nil {
			return Plan{}, err
		}
	}
	for _, f := range []File{r.FirmwareCode, r.FirmwareVars} {
		if err := verifyFile(f); err != nil {
			return Plan{}, err
		}
	}
	for _, path := range []string{r.SSHPrivateKey, r.SSHPublicKey} {
		if err := regularFile(path); err != nil {
			return Plan{}, err
		}
	}
	return Plan{
		SchemaVersion: 1, Selection: s, Template: template, Qualification: "unverified",
		Variables: map[string]any{
			"family": s.Family, "release": s.Version, "arch": s.Arch,
			"source_url": r.SourceURL, "source_sha256": r.SourceSHA256,
			"source_build": r.SourceBuild, "workspace": r.Workspace,
			"ssh_private_key_file": r.SSHPrivateKey, "ssh_public_key_file": r.SSHPublicKey,
			"efi_firmware_code": r.FirmwareCode.Path, "efi_firmware_vars": r.FirmwareVars.Path,
			"firmware_code_sha256": r.FirmwareCode.SHA256,
			"firmware_vars_sha256": r.FirmwareVars.SHA256,
			"accelerator":          r.Accelerator,
		},
	}, nil
}

func validate(r Request) error {
	if r.SchemaVersion != 1 || r.SourceBuild == "" || !digest.MatchString(r.SourceSHA256) {
		return fmt.Errorf(
			"%w: schemaVersion=1, sourceBuild and lowercase sourceSHA256 required",
			ErrInput,
		)
	}
	u, err := url.Parse(r.SourceURL)
	if !filepath.IsAbs(r.SourceURL) &&
		(err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "") {
		return fmt.Errorf(
			"%w: sourceURL must be an absolute local path or HTTPS without credentials, query or fragment",
			ErrInput,
		)
	}
	for _, path := range []string{r.Workspace, r.SSHPrivateKey, r.SSHPublicKey, r.FirmwareCode.Path, r.FirmwareVars.Path} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
			strings.ContainsAny(path, "\r\n") {
			return fmt.Errorf(
				"%w: clean absolute workspace, key and firmware paths required",
				ErrInput,
			)
		}
	}
	if r.SSHPrivateKey == r.SSHPublicKey {
		return fmt.Errorf("%w: separate public and private key files required", ErrInput)
	}
	switch r.Accelerator {
	case "kvm", "hvf", "tcg":
	default:
		return fmt.Errorf("%w: accelerator must be kvm, hvf or tcg", ErrInput)
	}
	return nil
}

func regularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: stat input: %w", ErrInput, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("%w: nonempty regular input required: %s", ErrInput, path)
	}
	return nil
}

func verifyFile(f File) error {
	if !digest.MatchString(f.SHA256) {
		return fmt.Errorf("%w: lowercase input SHA256 required", ErrInput)
	}
	if err := regularFile(f.Path); err != nil {
		return err
	}
	input, err := os.Open(f.Path)
	if err != nil {
		return fmt.Errorf("%w: read input: %w", ErrInput, err)
	}
	defer input.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return fmt.Errorf("%w: hash input: %w", ErrInput, err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%w: input checksum mismatch: %s", ErrInput, f.Path)
	}
	return nil
}

// resolveNative freezes aliases without requiring Linux firmware or SSH credentials.
func resolveNative(r Request) (Plan, error) {
	if r.SchemaVersion != 1 {
		return Plan{}, fmt.Errorf("%w: schemaVersion=1 required", ErrInput)
	}
	s, err := catalog.Current().Resolve(r.Family, r.Release, r.Arch)
	if err != nil {
		return Plan{}, err
	}
	template, err := s.Template(r.Purpose, r.Target)
	if err != nil {
		return Plan{}, err
	}
	if r.Timeout == "" {
		r.Timeout = "2h"
	}
	c := nativebuild.Config{
		Family:          s.Family,
		Release:         s.Version,
		Arch:            s.Arch,
		SourcePath:      r.SourceURL,
		SourceSHA256:    r.SourceSHA256,
		SourceBuild:     r.SourceBuild,
		Edition:         r.Edition,
		Language:        r.Language,
		Timeout:         r.Timeout,
		OutputDirectory: filepath.Join(r.Workspace, "candidate"),
	}
	if !filepath.IsAbs(r.Workspace) || filepath.Clean(r.Workspace) != r.Workspace {
		return Plan{}, fmt.Errorf("%w: clean absolute workspace required", ErrInput)
	}
	if err := c.Validate(); err != nil {
		return Plan{}, err
	}
	if err := verifyFile(File{Path: c.SourcePath, SHA256: c.SourceSHA256}); err != nil {
		return Plan{}, err
	}
	variables := map[string]any{
		"release":          c.Release,
		"arch":             c.Arch,
		"source_path":      c.SourcePath,
		"source_sha256":    c.SourceSHA256,
		"source_build":     c.SourceBuild,
		"output_directory": c.OutputDirectory,
		"timeout":          c.Timeout,
	}
	if r.Family == "windows-11" {
		variables["edition"] = c.Edition
		variables["language"] = c.Language
	}
	return Plan{
		SchemaVersion: 1,
		Selection:     s,
		Template:      template,
		Qualification: "unverified",
		Variables:     variables,
	}, nil
}
