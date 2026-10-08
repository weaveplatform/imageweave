package packerplugin
import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "github.com/hashicorp/packer-plugin-sdk/packer"
 "github.com/zclconf/go-cty/cty"
 ocispec "github.com/opencontainers/image-spec/specs-go/v1"
 "github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
 "github.com/weaveplatform/weaveplatform-oci/pkg/pack"
 "github.com/weaveplatform/weaveplatform-oci/pkg/spec"
 "github.com/weaveplatform/imageweave/internal/imagebuild/macos"
 "github.com/weaveplatform/imageweave/pkg/nativebuild"
)
func mustPrepared(t *testing.T,err error){t.Helper();if err!=nil{t.Fatal(err)}}
func preparedParent(t *testing.T) (macos.Parent, spec.Config) {
	t.Helper()
	dir := t.TempDir()
	mustPrepared(t, os.WriteFile(filepath.Join(dir, "disk0.img"), make([]byte, 4096), 0o600))
	mustPrepared(
		t,
		os.WriteFile(
			filepath.Join(dir, "auxstorage.bin"),
			[]byte("private writable firmware"),
			0o600,
		),
	)
	f := pack.BundleFile{
		SchemaVersion: 1,
		Annotations: map[string]string{
			spec.AnnotationVersion:  "test-r1",
			spec.AnnotationRevision: strings.Repeat("a", 40),
			spec.AnnotationSource:   "https://github.com/weaveplatform/imageweave",
		},
		Guest: spec.Guest{
			OS:        "darwin",
			Arch:      "arm64",
			OSVersion: "26.6.2",
			OSBuild:   "25G83",
			Variant:   "base",
		},
		Firmware: spec.Firmware{Type: "apple", TPM: "none", HardwareModel: "YnBsaXN0MDA="},
		Resources: spec.Resources{
			CPU:    spec.MinDefault{Min: 2, Default: 4},
			Memory: spec.MinDefault{Min: 4 << 30, Default: 4 << 30},
		},
		Provisioning: spec.Provisioning{CredentialHint: "set-at-first-boot"},
		Build: spec.Build{
			Template:    "templates/macos/image.pkr.hcl",
			TemplateRef: "repo@commit",
			Created:     "2026-10-08T00:00:00Z",
			SourceMedia: []spec.SourceMedia{
				{
					URI:    "https://vendor.example/base",
					Digest: "sha256:" + strings.Repeat("a", 64),
					Kind:   "ipsw",
				},
			},
		},
		Disks: []pack.BundleDisk{{Name: "disk0", Role: "system", Path: "disk0.img"}},
		State: []pack.BundleState{
			{
				Name:      spec.StateAuxStorage,
				Path:      "auxstorage.bin",
				Semantics: spec.SemanticsCarry,
				Required:  true,
			},
		},
	}
	mustPrepared(t, pack.WriteBundleFile(dir, f))
	b, err := pack.LoadBundle(dir)
	mustPrepared(t, err)
	layout := filepath.Join(t.TempDir(), "layout")
	store, err := pack.OpenLayout(t.Context(), layout)
	mustPrepared(t, err)
	child, err := pack.Manifest(t.Context(), b, store, chunk.Options{})
	mustPrepared(t, err)
	root, err := pack.Index(t.Context(), store, []ocispec.Descriptor{child}, nil)
	mustPrepared(t, err)
	mustPrepared(t, store.Tag(t.Context(), root, "base"))
	description, err := pack.Describe(t.Context(), store, child)
	mustPrepared(t, err)
	return macos.Parent{
		Layout: layout,
		Ref:    "base",
		Name:   "ghcr.io/example/macos-26-base",
		Digest: child.Digest.String(),
	}, description.Config
}
type preparedCommunicator struct {
	calls   int
	fail    int
	scripts []string
	output  string
}

func (c *preparedCommunicator) Start(_ context.Context, cmd *packer.RemoteCmd) error {
	c.calls++
	c.scripts = append(c.scripts, cmd.Command)
	if c.calls == c.fail {
		return macos.ErrPrepared
	}
	if cmd.Stdout != nil {
		_, _ = io.WriteString(cmd.Stdout, c.output)
	}
	cmd.SetExited(0)
	return nil
}
func (*preparedCommunicator) Upload(string, io.Reader, *os.FileInfo) error { return nil }
func (*preparedCommunicator) UploadDir(string, string, []string) error     { return nil }
func (*preparedCommunicator) Download(string, io.Writer) error             { return nil }
func (*preparedCommunicator) DownloadDir(string, string, []string) error   { return nil }

type preparedSession struct {
	comm        preparedCommunicator
	calls       []string
	fail        string
	observation macos.Observation
	second      *macos.Observation
	observed    int
	setup       []bool
}

func (v *preparedSession) step(name string) error {
	v.calls = append(v.calls, name)
	if v.fail == name {
		return macos.ErrPrepared
	}
	return nil
}

func (v *preparedSession) Connect(_ context.Context, setup bool) (packer.Communicator, error) {
	v.setup = append(v.setup, setup)
	return &v.comm, v.step(fmt.Sprintf("connect%d", len(v.setup)))
}

func (v *preparedSession) Observe(_ context.Context, marker string) (macos.Observation, error) {
	v.observed++
	o := v.observation
	if v.observed == 2 && v.second != nil {
		o = *v.second
	}
	o.Marker = marker
	return o, v.step(fmt.Sprintf("observe%d", v.observed))
}
func (v *preparedSession) Restart(context.Context) error { return v.step("restart") }
func (v *preparedSession) Stop(context.Context) error    { return v.step("stop") }
func (v *preparedSession) Close() error                  { return v.step("close") }
func (v *preparedSession) Identities() map[string]string {
	return map[string]string{"machineIdentifier": "native-id", "macAddress": "02:01:02:03:04:05"}
}

func preparedObservation() macos.Observation {
	return macos.Observation{
		Version:           "26.6.2",
		Build:             "25G83",
		HardwareUUID:      "hardware-uuid",
		ConsoleUser:       "weave",
		Groups:            "staff admin",
		AutoLoginUser:     "weave",
		SetupComplete:     true,
		AgentAbsent:       true,
		BuildAccessAbsent: true,
		HostKeyDigest:     "sha256:" + strings.Repeat("b", 64),
	}
}

func preparedFactory(v *preparedSession) macos.StartSession {
	return func(context.Context, string, spec.Config, io.Writer) (macos.Session, error) { return v, nil }
}


func TestPreparedBuilderConfigAndLifecycle(t *testing.T){
 parent,_:=preparedParent(t)
 values:=map[string]any{"parent_layout":parent.Layout,"parent_ref":parent.Ref,"parent_name":parent.Name,"parent_digest":parent.Digest,"output_directory":filepath.Join(t.TempDir(),"prepared"),"timeout":"1m"}
 b:=&PreparedBuilder{}
 if len(b.ConfigSpec())!=6{t.Fatal(b.ConfigSpec())}
 _,_,err:=b.Prepare(values);mustPrepared(t,err)
 hcl:=map[string]cty.Value{};for k,v:=range values{hcl[k]=cty.StringVal(v.(string))}
 _,_,err=b.Prepare(cty.ObjectVal(hcl));mustPrepared(t,err)
 for _,bad:=range []any{map[string]any{"unknown":true},map[string]any{"timeout":"bad"},cty.StringVal("bad"),cty.ObjectVal(map[string]cty.Value{"parent_ref":cty.UnknownVal(cty.String)})}{if _,_,err:=b.Prepare(bad);err==nil{t.Fatal("accepted invalid config")}}
 ui:=&packer.BasicUi{Reader:strings.NewReader(""),Writer:io.Discard,ErrorWriter:io.Discard}
 for _,hook:=range []packer.Hook{nil,rejectPreparedHook{}}{
  values["output_directory"]=filepath.Join(t.TempDir(),"prepared")
  _,_,err=b.Prepare(values);mustPrepared(t,err)
  v:=&preparedSession{observation:preparedObservation()};b.start=preparedFactory(v)
  a,err:=b.Run(t.Context(),ui,hook)
  if hook!=nil{if err==nil{t.Fatal("ignored failed provisioner")};continue}
  mustPrepared(t,err)
  if !strings.Contains(a.String(),"prepared") || len(a.Files())!=3 {t.Fatal(a)}
  data,err:=os.ReadFile(filepath.Join(a.Id(),"build-result.json"));mustPrepared(t,err)
  var result nativebuild.Result;mustPrepared(t,json.Unmarshal(data,&result))
  if result.SchemaVersion!=2 || result.Prepared.Parent.Digest!=parent.Digest || result.Qualification!="unverified" || result.Prepared.User!="weave"{t.Fatal(result)}
 }
 values["parent_ref"]="absent";_,_,err=b.Prepare(values);mustPrepared(t,err);b.start=nil
 if _,err=b.Run(t.Context(),ui,nil);err==nil{t.Fatal("accepted missing parent")}
}

type rejectPreparedHook struct{}
func(rejectPreparedHook)Run(context.Context,string,packer.Ui,packer.Communicator,any)error{return macos.ErrPrepared}
