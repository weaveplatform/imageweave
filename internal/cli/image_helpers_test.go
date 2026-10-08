package cli_test

import (
	"bytes"
	"testing"

	"github.com/weaveplatform/imageweave/internal/cli"
)

func runImage(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := cli.New(&out, &stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	code := 0
	if err != nil {
		code = 1
		if len(err.Error()) >= 5 && err.Error()[:5] == "usage" {
			code = 2
		}
		stderr.WriteString(err.Error())
	}
	return code, out.String(), stderr.String()
}
