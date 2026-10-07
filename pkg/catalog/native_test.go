package catalog_test

import (
	"testing"

	"github.com/weaveplatform/imageweave/pkg/catalog"
)

func TestNativeTemplateMatrix(t *testing.T) {
	count := 0
	for _, s := range catalog.Current().Matrix() {
		target := "hcs"
		if s.Family == "macos" {
			target = "apple-vz"
		} else if s.Family != "windows-11" {
			continue
		}
		path, err := s.Template("guest-base", target)
		if s.Status == "template" {
			if err != nil || path == "" {
				t.Fatal(s, err)
			}
			count++
		} else if err == nil {
			t.Fatal("unsupported combination accepted", s)
		}
	}
	if count != 9 {
		t.Fatal(count)
	}
}
