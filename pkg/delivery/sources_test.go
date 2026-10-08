package delivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/catalog"
)

func TestEveryFedoraCatalogRowHasImmutableAuthenticatedPin(t *testing.T) {
	root := filepath.Join("..", "..")
	fingerprints := map[string]string{"44": "36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6", "43": "C6E7F081CF80E13146676E88829B606631645531", "42": "B0F4950458F69E1150C6C5EDC8AC4916105EF944"}
	count := 0
	for _, row := range catalog.Current().Matrix() {
		if row.Family != "fedora" {
			continue
		}
		count++
		data, err := os.ReadFile(filepath.Join(root, "delivery", "sources", "fedora-"+row.Version+"-"+row.Arch+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var pin struct {
			SourceURL    string
			SourceSHA256 string
			SourceBuild  string
			ChecksumsURL string
			Fingerprint  string
			Keyring      string
		}
		if err := json.Unmarshal(data, &pin); err != nil {
			t.Fatal(err)
		}
		if len(pin.SourceSHA256) != 64 || pin.Fingerprint != fingerprints[row.Version] || !strings.Contains(pin.SourceURL, "-Generic-"+pin.SourceBuild) || !strings.Contains(pin.ChecksumsURL, "-"+pin.SourceBuild+"-") {
			t.Fatalf("invalid pin %+v", pin)
		}
		if row.Version == "42" && !strings.HasPrefix(pin.SourceURL, "https://archives.fedoraproject.org/") {
			t.Fatal("N-2 must retain archived source")
		}
		vendorarch := "aarch64"
		if row.Arch == "amd64" {
			vendorarch = "x86_64"
		}
		if !strings.HasSuffix(pin.SourceURL, "."+vendorarch+".qcow2") {
			t.Fatal(pin.SourceURL)
		}
		key, err := os.ReadFile(filepath.Join(root, pin.Keyring))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(key), "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
			t.Fatal("missing armored release key")
		}
	}
	if count != 6 {
		t.Fatal(count)
	}
}
