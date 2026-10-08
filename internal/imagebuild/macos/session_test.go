package macos

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/packer"
	"golang.org/x/crypto/ssh"

	"github.com/weaveplatform/weaveplatform-oci/pkg/macsetup"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

type fakeMachine struct {
	fail   string
	native []bool
	inputs int
	frame  macsetup.Screen
	closed bool
}

func (m *fakeMachine) start(_ context.Context, native bool) error {
	m.native = append(m.native, native)
	if m.fail == "start" {
		return ErrPrepared
	}
	return nil
}

func (m *fakeMachine) stop(_ context.Context, force bool) error {
	if m.fail == "stop" || (force && m.fail == "force") {
		return ErrPrepared
	}
	return nil
}
func (m *fakeMachine) identity() (string, string) { return "machine-id", "02:01:02:03:04:05" }
func (m *fakeMachine) screen(context.Context) (macsetup.Screen, error) {
	if m.fail == "screen" {
		return m.frame, ErrPrepared
	}
	return m.frame, nil
}

func (m *fakeMachine) input(context.Context, []macsetup.Action) error {
	m.inputs++
	if m.fail == "input" {
		return ErrPrepared
	}
	return nil
}
func (m *fakeMachine) close() error {
	m.closed = true
	if m.fail == "close" {
		return ErrPrepared
	}
	return nil
}

func fastSession(m *fakeMachine, c *fakeCommunicator) *nativeSession {
	s := &nativeSession{
		vm:  m,
		cfg: spec.Config{Guest: spec.Guest{OSVersion: "27.0", OSBuild: "26A1"}},
		key: []byte("test host key"),
		log: io.Discard,
	}
	now := time.Unix(1, 0)
	s.calls = sessionClock{
		now:     func() time.Time { now = now.Add(30 * time.Second); return now },
		wait:    func(context.Context, time.Duration) error { return nil },
		connect: func(context.Context) (packer.Communicator, error) { return c, nil },
	}
	return s
}

func TestSessionRequiresSettledDesktopAndPreservesRebootIdentity(t *testing.T) {
	m := &fakeMachine{}
	c := &fakeCommunicator{output: "27.0\n26A1\nweave\nVirtualMac2,1\n"}
	s := fastSession(m, c)
	_, err := s.Connect(t.Context(), true)
	must(t, err)
	if len(m.native) != 1 || !m.native[0] || m.inputs != 0 {
		t.Fatal(m)
	}
	must(t, s.Restart(t.Context()))
	_, err = s.Connect(t.Context(), false)
	must(t, err)
	if len(m.native) != 2 || m.native[1] {
		t.Fatal("re-applied native provisioning after reboot", m.native)
	}
	c.output = "27.0\n26A1\nchallenge\nhardware-uuid\nweave\nstaff admin\nweave\nyes\nyes\nyes\n"
	observed, err := s.Observe(t.Context(), "challenge")
	must(t, err)
	if observed.Marker != "challenge" || !strings.HasPrefix(observed.HostKeyDigest, "sha256:") ||
		s.Identities()["machineIdentifier"] != "machine-id" {
		t.Fatal(observed)
	}
	must(t, s.Stop(t.Context()))
	must(t, s.Stop(t.Context()))
	must(t, s.Close())
	if !m.closed {
		t.Fatal("native runtime not closed")
	}
}

func TestSessionFailureAndCancellation(t *testing.T) {
	for _, fail := range []string{"start", "screen", "input", "stop", "close"} {
		t.Run(fail, func(t *testing.T) {
			m := &fakeMachine{
				fail: fail,
				frame: macsetup.Screen{
					Width:  1024,
					Height: 768,
					Text:   []macsetup.Text{{Value: "Finder"}},
				},
			}
			c := &fakeCommunicator{output: "not ready"}
			s := fastSession(m, c)
			if fail == "stop" {
				s.started = true
				s.comm = c
				if s.Stop(t.Context()) == nil {
					t.Fatal("ignored stop failure")
				}
				if s.Restart(t.Context()) == nil {
					t.Fatal("ignored restart failure")
				}
				return
			}
			if fail == "close" {
				if s.Close() == nil {
					t.Fatal("ignored close failure")
				}
				return
			}
			if _, err := s.Connect(t.Context(), true); err == nil {
				t.Fatal("ignored native failure")
			}
			_ = s.Close()
		})
	}
	m := &fakeMachine{frame: macsetup.Screen{Text: []macsetup.Text{{Value: "Finder"}}}}
	s := fastSession(m, &fakeCommunicator{})
	s.calls.connect = func(context.Context) (packer.Communicator, error) { return nil, ErrPrepared }
	s.calls.wait = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := s.Connect(t.Context(), true); err == nil || m.inputs != 1 {
		t.Fatal(err, m)
	}
	m.inputs = 0
	if _, err := s.Connect(t.Context(), false); err == nil || m.inputs != 0 {
		t.Fatal("prepared guest received setup input", err, m)
	}
	s = &nativeSession{vm: m}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Connect(ctx, false); err == nil {
		t.Fatal("cancelled connect succeeded")
	}
	if _, err := s.Observe(ctx, "x"); err == nil {
		t.Fatal("unconnected observation")
	}
	s.started = true
	if s.Stop(ctx) == nil {
		t.Fatal("unauthenticated shutdown")
	}
	c := &fakeCommunicator{fail: 1}
	s.comm = c
	if s.Stop(ctx) == nil {
		t.Fatal("ignored shutdown request failure")
	}
	c.calls = 0
	if _, err := s.Observe(ctx, "challenge"); err == nil {
		t.Fatal("ignored observation failure")
	}
	c.calls = 0
	if s.desktopReady(ctx) == nil {
		t.Fatal("ignored desktop probe failure")
	}
	if _, err := s.Observe(ctx, "unsafe ' input"); err == nil {
		t.Fatal("unsafe challenge")
	}
	s = fastSession(&fakeMachine{fail: "start"}, &fakeCommunicator{})
	if s.Restart(ctx) == nil {
		t.Fatal("ignored restart start failure")
	}
	if _, err := s.connectSSH(ctx); err == nil {
		t.Fatal("unexpected lease for fake machine")
	}
}

func sshServer(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	must(t, err)
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if c.User() != "weave" || string(password) != "weave" {
				return nil, ErrPrepared
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					_ = ch.Reject(ssh.UnknownChannelType, "not needed for authentication test")
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestPackerSSHPinsAuthenticatedHostKey(t *testing.T) {
	s := &nativeSession{vm: &fakeMachine{}}
	first := sshServer(t)
	_, err := s.sshConnection(t.Context(), first)
	must(t, err)
	if len(s.key) == 0 {
		t.Fatal("authenticated host key not retained")
	}
	key := append([]byte(nil), s.key...)
	_, err = s.sshConnection(t.Context(), first)
	must(t, err)
	if _, err = s.sshConnection(t.Context(), sshServer(t)); err == nil {
		t.Fatal("changed host key accepted")
	}
	if !bytes.Equal(key, s.key) {
		t.Fatal("failed authentication replaced trust")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.sshConnection(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("cancelled dial succeeded")
	}
	must(t, s.Close())
}

func TestGuestCommandRejectsFailedExitAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if runGuest(ctx, &fakeCommunicator{}, "true") == nil {
		t.Fatal("cancelled command succeeded")
	}
	if err := runGuest(t.Context(), &exitCommunicator{}, "false"); err == nil {
		t.Fatal("failed exit accepted")
	}
	s := &nativeSession{vm: &fakeMachine{}, comm: &exitCommunicator{}}
	if _, err := s.Observe(t.Context(), "challenge"); err == nil {
		t.Fatal("failed observation exit accepted")
	}
	if s.desktopReady(t.Context()) == nil {
		t.Fatal("failed desktop exit accepted")
	}
}

type exitCommunicator struct{ fakeCommunicator }

func (*exitCommunicator) Start(_ context.Context, c *packer.RemoteCmd) error {
	c.SetExited(1)
	return nil
}

var _ = fmt.Sprintf
