// Package catalog resolves reviewed release windows without moving upstream selectors.
package catalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed releases.json
var data []byte

var (
	ErrSelection = errors.New("unknown image selection")
	ErrBuild     = errors.New("image scenario is not implemented")
)

// Catalog is the versioned release policy reviewed with the recipes.
type Catalog struct {
	SchemaVersion int      `json:"schemaVersion"`
	ReviewedAt    string   `json:"reviewedAt"`
	Families      []Family `json:"families"`
}

// Family defines an ordered track; macOS version numbers need not be consecutive.
type Family struct {
	ID       string    `json:"id"`
	Track    string    `json:"track"`
	Source   string    `json:"source"`
	Releases []Release `json:"releases"`
}

// Release keeps maintenance separate from builder readiness.
type Release struct {
	Version       string         `json:"version"`
	Slot          string         `json:"slot"`
	Maintenance   string         `json:"maintenance"`
	Architectures []Architecture `json:"architectures"`
}

// Architecture records a requested target, including unsupported combinations.
type Architecture struct {
	Arch   string `json:"arch"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// Selection contains the exact resolution of a release selector.
type Selection struct {
	Family      string `json:"family"`
	Track       string `json:"track"`
	Version     string `json:"version"`
	Slot        string `json:"slot"`
	Arch        string `json:"arch"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Maintenance string `json:"maintenance"`
	ReviewedAt  string `json:"reviewedAt"`
}

// Current returns a fresh copy so callers cannot change another plan's catalog.
func Current() Catalog {
	var c Catalog
	// Compiled data is checked by the catalog contract test.
	if err := json.Unmarshal(data, &c); err != nil {
		panic(err)
	}
	return c
}

// Matrix includes unimplemented and unsupported targets rather than hiding them.
func (c Catalog) Matrix() []Selection {
	var out []Selection
	for _, f := range c.Families {
		for _, r := range f.Releases {
			for _, a := range r.Architectures {
				out = append(out, Selection{
					Family: f.ID, Track: f.Track, Version: r.Version, Slot: r.Slot,
					Arch: a.Arch, Status: a.Status, Reason: a.Reason,
					Maintenance: r.Maintenance, ReviewedAt: c.ReviewedAt,
				})
			}
		}
	}
	return out
}

// Resolve accepts a concrete release or n/n-1/n-2 and returns a concrete version.
func (c Catalog) Resolve(family, release, arch string) (Selection, error) {
	for _, s := range c.Matrix() {
		if s.Family == family && s.Arch == arch &&
			(s.Version == release || s.Slot == strings.ToLower(release)) {
			return s, nil
		}
	}
	return Selection{}, fmt.Errorf("%w: %s/%s/%s", ErrSelection, family, release, arch)
}

// Template permits only an implemented scenario. Listed targets are not build promises.
func (s Selection) Template(purpose, target string) (string, error) {
	if s.Status == "template" && purpose == "guest-prepared" && s.Family == "macos" &&
		s.Arch == "arm64" &&
		target == "apple-vz" {
		return "templates/macos/prepared", nil
	}
	if s.Status != "template" || purpose != "guest-base" {
		return "", fmt.Errorf("%w: %s/%s/%s purpose=%s target=%s: %s",
			ErrBuild, s.Family, s.Version, s.Arch, purpose, target, s.Reason)
	}
	switch {
	case (s.Family == "ubuntu" || s.Family == "fedora") && target == "qemu":
		return "templates/qemu", nil
	case s.Family == "windows-11" && target == "hcs":
		return "templates/windows", nil
	case s.Family == "macos" && s.Arch == "arm64" && target == "apple-vz":
		return "templates/macos", nil
	default:
		return "", fmt.Errorf(
			"%w: incompatible target %s for %s/%s",
			ErrBuild,
			target,
			s.Family,
			s.Arch,
		)
	}
}
