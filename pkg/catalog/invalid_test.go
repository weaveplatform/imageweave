package catalog

import "testing"

func TestInvalidEmbeddedCatalogFailsClosed(t *testing.T) {
	original := data
	t.Cleanup(func() { data = original })
	data = []byte("{invalid")
	defer func() {
		if recover() == nil {
			t.Fatal("invalid embedded catalog became an empty successful catalog")
		}
	}()
	Current()
}
