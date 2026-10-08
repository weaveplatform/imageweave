package macosbuild

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCandidatePolicyAcceptsOnlyReviewedMacOSWorkflow(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "delivery", "macos-candidate-policy.json"))
	must(t, err)
	var policy struct {
		Registry    string `json:"registry"`
		TrustedRoot string `json:"trustedRoot"`
		Build       struct{ Issuer, SubjectRegexp string }
		Acceptance  struct{ Issuer, SubjectRegexp string }
	}
	must(t, json.Unmarshal(raw, &policy))
	if policy.Registry != "ghcr.io" || policy.TrustedRoot == "" {
		t.Fatal("candidate requires the reviewed registry and public trust root")
	}
	_, err = os.Stat(filepath.Join("..", "..", "delivery", policy.TrustedRoot))
	must(t, err)
	for _, identity := range []struct{ Issuer, SubjectRegexp string }{policy.Build, policy.Acceptance} {
		if identity.Issuer != "https://token.actions.githubusercontent.com" {
			t.Fatal("unexpected identity issuer")
		}
		pattern, err := regexp.Compile(identity.SubjectRegexp)
		must(t, err)
		for subject, allowed := range map[string]bool{
			"https://github.com/weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/heads/main":      true,
			"https://github.com/weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/heads/test":      false,
			"https://github.com/weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/pull/12/merge":   false,
			"https://github.com/weaveplatform/imageweave/.github/workflows/linux-candidate.yml@refs/heads/main":      false,
			"https://github.com/other/imageweave/.github/workflows/macos-candidate.yml@refs/heads/main":              false,
			"https://github.com/weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/heads/main-evil": false,
			"https://githubXcom/weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/heads/main":      false,
		} {
			if pattern.MatchString(subject) != allowed {
				t.Errorf("unexpected trust decision for %s", subject)
			}
		}
	}
}
