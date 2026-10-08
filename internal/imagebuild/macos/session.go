package macos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/packer"
	packerssh "github.com/hashicorp/packer-plugin-sdk/sdk-internals/communicator/ssh"
	"golang.org/x/crypto/ssh"

	"github.com/weaveplatform/weaveplatform-oci/pkg/macsetup"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

type machine interface {
	start(context.Context, bool) error
	stop(context.Context, bool) error
	identity() (string, string)
	screen(context.Context) (macsetup.Screen, error)
	input(context.Context, []macsetup.Action) error
	close() error
}

type sessionClock struct {
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
	connect func(context.Context) (packer.Communicator, error)
}

type nativeSession struct {
	calls       sessionClock
	vm          machine
	cfg         spec.Config
	log         io.Writer
	started     bool
	comm        packer.Communicator
	key         []byte
	connections []net.Conn
	mu          sync.Mutex
}

func (s *nativeSession) Connect(ctx context.Context, setup bool) (packer.Communicator, error) {
	if s.calls.now == nil {
		s.calls.now = time.Now
	}
	if s.calls.wait == nil {
		s.calls.wait = pause
	}
	if s.calls.connect == nil {
		s.calls.connect = s.connectSSH
	}
	if !s.started {
		native := setup && strings.HasPrefix(s.cfg.Guest.OSVersion, "27.")
		s.started = true
		if err := s.vm.start(ctx, native); err != nil {
			return nil, err
		}
		s.started = true
	}
	lastStage := ""
	var ready macsetup.Readiness
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for guest SSH: %w", err)
		}
		if comm, err := s.calls.connect(ctx); err == nil {
			s.comm = comm
			valid := s.desktopReady(ctx) == nil
			if ready.Observe(s.calls.now(), valid) {
				return comm, nil
			}
			if valid {
				if err := s.calls.wait(ctx, 3*time.Second); err != nil {
					return nil, err
				}
				continue
			}
		}
		ready.Observe(s.calls.now(), false)
		if setup {
			screen, err := s.vm.screen(ctx)
			if err != nil {
				return nil, err
			}
			p, err := macsetup.Next(
				screen,
				macsetup.Config{
					User:      "weave",
					Password:  "weave",
					AutoLogin: true,
					Version:   s.cfg.Guest.OSVersion,
					Build:     s.cfg.Guest.OSBuild,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("setup assistant: %w", err)
			}
			if p.Stage != lastStage && s.log != nil {
				_, _ = fmt.Fprintf(s.log, "macOS setup: %s\n", p.Stage)
				lastStage = p.Stage
			}
			if p.Stage == "desktop" {
				p.Actions = append(
					macsetup.DesktopFocus(screen),
					macsetup.Action{Kind: "key", Value: "cmd+space"},
					macsetup.Action{Kind: "type", Value: "Terminal.app"},
				)
			}
			if err = s.vm.input(ctx, p.Actions); err != nil {
				return nil, err
			}
		}
		if err := s.calls.wait(ctx, 3*time.Second); err != nil {
			return nil, err
		}
	}
}

func (s *nativeSession) connectSSH(ctx context.Context) (packer.Communicator, error) {
	_, mac := s.vm.identity()
	ip, err := leaseAddress(mac)
	if err != nil {
		return nil, err
	}
	return s.sshConnection(ctx, net.JoinHostPort(ip, "22"))
}

func (s *nativeSession) sshConnection(
	ctx context.Context,
	address string,
) (packer.Communicator, error) {
	var observed []byte
	comm, err := packerssh.New(address, &packerssh.Config{
		SSHConfig: &ssh.ClientConfig{
			User:              "weave",
			Auth:              []ssh.AuthMethod{ssh.Password("weave")},
			HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				s.mu.Lock()
				defer s.mu.Unlock()
				if len(s.key) > 0 && !bytes.Equal(s.key, key.Marshal()) {
					return fmt.Errorf("%w: guest SSH host key changed", ErrPrepared)
				}
				observed = append([]byte(nil), key.Marshal()...)
				return nil
			},
		},
		Connection: func() (net.Conn, error) {
			c, e := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
			if e != nil {
				return nil, fmt.Errorf("dial guest SSH: %w", e)
			}
			s.mu.Lock()
			s.connections = append(s.connections, c)
			s.mu.Unlock()
			return c, nil
		},
		DisableAgentForwarding: true,
		HandshakeTimeout:       5 * time.Second,
		Timeout:                30 * time.Second,
		KeepAliveInterval:      -1,
		UseSftp:                true,
	})
	if err != nil {
		return nil, fmt.Errorf("authenticate guest SSH: %w", err)
	}
	s.mu.Lock()
	if len(s.key) == 0 {
		s.key = observed
	}
	s.mu.Unlock()
	return comm, nil
}

func leaseAddress(mac string) (string, error) {
	raw, err := os.ReadFile("/var/db/dhcpd_leases")
	if err != nil {
		return "", fmt.Errorf("read Apple DHCP leases: %w", err)
	}
	return parseLease(string(raw), mac)
}

func parseLease(raw, mac string) (string, error) {
	wanted, err := net.ParseMAC(mac)
	if err != nil {
		return "", fmt.Errorf("parse guest MAC: %w", err)
	}
	for _, block := range strings.Split(raw, "}") {
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok {
				values[k] = v
			}
		}
		_, hw, _ := strings.Cut(values["hw_address"], ",")
		// bootpd omits leading zeroes in each Ethernet octet.
		parts := strings.Split(hw, ":")
		for i := range parts {
			if len(parts[i]) == 1 {
				parts[i] = "0" + parts[i]
			}
		}
		observed, e := net.ParseMAC(strings.Join(parts, ":"))
		if e == nil && bytes.Equal(wanted, observed) && net.ParseIP(values["ip_address"]) != nil {
			return values["ip_address"], nil
		}
	}
	return "", fmt.Errorf("%w: no DHCP lease for guest", ErrPrepared)
}

var challengePattern = regexp.MustCompile(`^[A-Z0-9a-z-]+$`)

func (s *nativeSession) Observe(ctx context.Context, marker string) (Observation, error) {
	if s.comm == nil || !challengePattern.MatchString(marker) {
		return Observation{}, fmt.Errorf(
			"%w: connected guest and safe challenge required",
			ErrPrepared,
		)
	}
	var out bytes.Buffer
	cmd := &packer.RemoteCmd{
		Command: fmt.Sprintf(observationScript, marker),
		Stdout:  &out,
		Stderr:  io.Discard,
	}
	if err := s.comm.Start(ctx, cmd); err != nil {
		return Observation{}, fmt.Errorf("observe prepared guest: %w", err)
	}
	cmd.Wait()
	if cmd.ExitStatus() != 0 {
		return Observation{}, fmt.Errorf(
			"%w: guest observation exited %d",
			ErrPrepared,
			cmd.ExitStatus(),
		)
	}
	s.mu.Lock()
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(s.key))
	s.mu.Unlock()
	return parseObservation(out.String(), hash)
}

func (s *nativeSession) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if err := s.vm.start(ctx, false); err != nil {
		return err
	}
	s.started = true
	return nil
}

func (s *nativeSession) Stop(ctx context.Context) error {
	if !s.started {
		return nil
	}
	if s.comm == nil {
		return fmt.Errorf("%w: shutdown requires authenticated guest", ErrPrepared)
	}
	cmd := &packer.RemoteCmd{
		Command: "printf '%s\\n' weave | sudo -S -p '' /sbin/shutdown -h now",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
	if err := s.comm.Start(ctx, cmd); err != nil {
		return fmt.Errorf("request guest shutdown: %w", err)
	}
	cmd.Wait()
	// Wait for the OS shutdown; a forced VM stop is never successful sealing.
	if err := s.vm.stop(ctx, false); err != nil {
		return err
	}
	s.started = false
	s.comm = nil
	return nil
}

func (s *nativeSession) Close() error {
	s.mu.Lock()
	for _, c := range s.connections {
		_ = c.Close()
	}
	s.connections = nil
	s.mu.Unlock()
	// Cleanup deliberately survives cancellation of the build context.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var err error
	if s.started {
		err = s.vm.stop(ctx, true)
	}
	return errors.Join(err, s.vm.close())
}

func (s *nativeSession) desktopReady(ctx context.Context) error {
	var out bytes.Buffer
	cmd := &packer.RemoteCmd{
		Command: `/usr/bin/sw_vers -productVersion; /usr/bin/sw_vers -buildVersion; /usr/bin/stat -f %Su /dev/console; /usr/sbin/sysctl -n hw.model; /bin/ps -axo user=,comm=`,
		Stdout:  &out,
		Stderr:  io.Discard,
	}
	if err := s.comm.Start(ctx, cmd); err != nil {
		return fmt.Errorf("probe desktop: %w", err)
	}
	cmd.Wait()
	if cmd.ExitStatus() != 0 {
		return fmt.Errorf("%w: desktop probe failed", ErrPrepared)
	}
	_, err := macsetup.VerifyIdentity(
		out.String(),
		macsetup.Config{User: "weave", Version: s.cfg.Guest.OSVersion, Build: s.cfg.Guest.OSBuild},
	)
	if err != nil {
		return fmt.Errorf("desktop identity: %w", err)
	}
	return nil
}

func (s *nativeSession) Identities() map[string]string {
	id, mac := s.vm.identity()
	return map[string]string{"machineIdentifier": id, "macAddress": mac}
}
