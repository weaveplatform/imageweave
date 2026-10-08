//go:build !windows

package macosbuild

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunnerGroupActivationFailsClosed(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("administrator bootstrap requires jq")
	}
	script, err := filepath.Abs(
		filepath.Join("..", "..", "scripts", "configure-macos-runner-group.sh"),
	)
	must(t, err)
	for _, mode := range []string{"success", "missing-workflow", "wrong-policy", "update-failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			must(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGroupAPI), 0o700))
			cmd := exec.CommandContext(t.Context(), "bash", script)
			cmd.Env = append(
				os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_DIR="+dir,
				"TEST_MODE="+mode,
			)
			output, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "success") {
				t.Fatalf("activation result %v: %s", err, output)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "repositories"))
			must(t, err)
			var state struct {
				IDs []int `json:"selected_repository_ids"`
			}
			must(t, json.Unmarshal(raw, &state))
			if mode == "success" {
				if len(state.IDs) != 1 || state.IDs[0] != 42 {
					t.Fatalf("wrong repository access: %s", raw)
				}
			} else if len(state.IDs) != 0 {
				t.Fatalf("failed configuration granted access: %s", raw)
			}
		})
	}
}

// The fake API retains requested policy and repository access. Real shell/jq
// execution verifies missing workflows and ineffective restrictions cannot
// enable a runner, without changing an organization during unit tests.
const fakeGroupAPI = `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'api orgs/weaveplatform/actions/runner-groups --paginate '*) echo 3 ;;
  'api --method PUT orgs/weaveplatform/actions/runner-groups/3/repositories --input -') cat > "$TEST_DIR/repositories" ;;
  'api --method PATCH orgs/weaveplatform/actions/runner-groups/3 --input -')
    cat > "$TEST_DIR/policy"
    if jq -e '.selected_workflows | length > 0' "$TEST_DIR/policy" >/dev/null && [ "$TEST_MODE" = update-failed ]; then exit 1; fi ;;
  'api repos/weaveplatform/imageweave/contents/'*) [ "$TEST_MODE" != missing-workflow ] ;;
  'api repos/weaveplatform/imageweave --jq .id') echo 42 ;;
  'api orgs/weaveplatform/actions/runner-groups/3')
    if [ "$TEST_MODE" = wrong-policy ]; then jq '.restricted_to_workflows=false' "$TEST_DIR/policy"; else cat "$TEST_DIR/policy"; fi ;;
  'api orgs/weaveplatform/actions/runner-groups/3/repositories')
    jq '{total_count:(.selected_repository_ids|length),repositories:[.selected_repository_ids[]|{id:.}]}' "$TEST_DIR/repositories" ;;
  *) echo "unexpected API request: $*" >&2; exit 2 ;;
esac
`
