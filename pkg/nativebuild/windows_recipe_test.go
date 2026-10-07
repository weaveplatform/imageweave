package nativebuild

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/deploymenttheory/go-sdk-winmediafoundry/pkg/udf"
)

func TestWindowsRecipeAndCompletion(t *testing.T) {
	_, s := windowsFixture()
	marker := "WEAVE-IMAGE-READY-0123456789abcdef01234567"
	for _, arch := range []string{"amd64", "arm64"} {
		for _, edition := range []string{"pro", "home", "enterprise", "education"} {
			s.Arch, s.Edition = arch, edition
			answer, script, err := windowsRecipe(s, marker)
			must(t, err)
			decoder := xml.NewDecoder(strings.NewReader(answer))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				must(t, err)
			}
			for _, want := range []string{`processorArchitecture="` + arch + `"`, `<Mode>Audit</Mode>`, `<WillWipeDisk>true</WillWipeDisk>`, `WEAVE-SEED`} {
				if !strings.Contains(answer, want) {
					t.Fatal(want)
				}
			}
			for _, want := range []string{"/generalize", "GENERALIZE_RESEAL_TO_OOBE", "bcdboot.exe", marker, "DisplayVersion", "FullyDecrypted"} {
				if !strings.Contains(script, want) {
					t.Fatal(want)
				}
			}
			for _, forbidden := range []string{"AutoLogon", "Password", "BypassTPM", "BypassSecureBoot"} {
				if strings.Contains(answer+script, forbidden) {
					t.Fatal(forbidden)
				}
			}
		}
	}
	if _, _, err := windowsRecipe(s, "predictable"); err == nil {
		t.Fatal("bad marker")
	}
	s.Arch = "other"
	if _, _, err := windowsRecipe(s, marker); err == nil {
		t.Fatal("bad architecture")
	}
	result := WindowsInstallResult{
		OSVersion:   "10.0.27000.1",
		Build:       "27000.1",
		Edition:     "Professional",
		Release:     "26H2",
		Generalized: true,
	}
	raw, err := json.Marshal(result)
	must(t, err)
	got, err := windowsReceipt(marker+" "+string(raw), marker)
	must(t, err)
	if got != result {
		t.Fatal(got)
	}
	for _, line := range []string{"wrong " + string(raw), marker + " invalid", marker + ` {"generalized":false}`} {
		if _, err := windowsReceipt(line, marker); err == nil {
			t.Fatal("accepted invalid receipt")
		}
	}
	_, s = windowsFixture()
	iso, err := windowsSeed(t.TempDir(), s, marker)
	must(t, err)
	stat, err := os.Stat(iso)
	must(t, err)
	if stat.Size() == 0 {
		t.Fatal("empty seed")
	}
	f, err := os.Open(iso)
	must(t, err)
	defer f.Close()
	volume, err := udf.Read(f)
	must(t, err)
	answer, script, err := windowsRecipe(s, marker)
	must(t, err)
	for name, want := range map[string]string{"autounattend.xml": answer, "seal.ps1": script} {
		got, err := volume.ReadFile([]string{name})
		must(t, err)
		if string(got) != want {
			t.Fatalf("seed file %s differs from recipe", name)
		}
	}
}

func TestWindowsHCSIsolationDocument(t *testing.T) {
	d := windowsDocument("disk", "install", "seed", "firmware", "serial")
	vm := d["VirtualMachine"].(map[string]any)
	sec := vm["SecuritySettings"].(map[string]any)
	if sec["EnableTpm"] != true || sec["Isolation"].(map[string]any)["HclEnabled"] != true {
		t.Fatal(sec)
	}
	uefi := vm["Chipset"].(map[string]any)["Uefi"].(map[string]any)
	if uefi["ApplySecureBootTemplate"] != "Apply" ||
		uefi["SecureBootTemplateId"] != "1734c6e8-3154-4dda-ba5f-a874cc483422" {
		t.Fatal(uefi)
	}
	if _, ok := uefi["BootThis"]; ok {
		t.Fatal("explicit boot entry incompatible with isolated HCS")
	}
	if _, ok := vm["Devices"].(map[string]any)["NetworkAdapters"]; ok {
		t.Fatal("installer should be offline")
	}
	if d["ShouldTerminateOnLastHandleClosed"] != true {
		t.Fatal("unmanaged VM lifetime")
	}
}
