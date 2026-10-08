package imagebuild

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeToolHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "image-helper" {
		return
	}
	fmt.Print("native output")
	os.Exit(0)
}

func TestNativeToolsAndFileRefusals(t *testing.T) {
	tools := Tools{Log: io.Discard}
	output, err := tools.Output(
		t.Context(),
		os.Args[0],
		"-test.run=TestNativeToolHelper",
		"--",
		"image-helper",
	)
	must(t, err)
	if !strings.HasPrefix(string(output), "native output") {
		t.Fatal(string(output))
	}
	if err := tools.RunCommand(
		t.Context(),
		nil,
		filepath.Join(t.TempDir(), "missing-tool"),
	); err == nil {
		t.Fatal("missing executable")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	must(t, os.WriteFile(src, []byte("source"), 0o600))
	if CopyFile("missing", filepath.Join(dir, "out")) == nil {
		t.Fatal("missing source")
	}
	if CopyFile(src, src) == nil {
		t.Fatal("overwritten source")
	}
	if CopyFile(dir, filepath.Join(dir, "bad-copy")) == nil {
		t.Fatal("copied directory as file")
	}
	if NewDirectory(filepath.Join(src, "child")) == nil {
		t.Fatal("parent is file")
	}
	if WriteJSON(dir, map[string]string{}) == nil {
		t.Fatal("wrote JSON over directory")
	}
	if WriteJSON(filepath.Join(dir, "json"), make(chan int)) == nil {
		t.Fatal("encoded channel")
	}
	if WriteNewJSON(filepath.Join(dir, "json"), make(chan int)) == nil {
		t.Fatal("encoded channel")
	}
	if CopyTree("missing", filepath.Join(dir, "tree")) == nil {
		t.Fatal("missing payload")
	}
	if CopyTree(dir, src) == nil {
		t.Fatal("payload destination is file")
	}
	if got := ShellQuote("a'b"); got != "'a'\"'\"'b'" {
		t.Fatal(got)
	}
}
