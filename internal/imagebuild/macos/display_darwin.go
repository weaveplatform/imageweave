//go:build darwin && arm64

package macos

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"strings"
	"time"

	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/vision"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/libraries/dispatch"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/obj"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/purego"
	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/mainthread"
	vnc "github.com/mitchellh/go-vnc"

	"github.com/weaveplatform/weaveplatform-oci/pkg/macsetup"
)

// Apple exposes no public headless framebuffer/input API. Tart and Guestweave
// use _VZVNCServer; isolate that private API here and fail when it is unavailable.
type appleDisplay struct {
	server   purego.ID
	client   displayClient
	calls    *displayCalls
	conn     net.Conn
	messages chan vnc.ServerMessage
	frame    *image.RGBA
}

type displayClient interface {
	inputClient
	FramebufferUpdateRequest(bool, uint16, uint16, uint16, uint16) error
	SetPixelFormat(*vnc.PixelFormat) error
	SetEncodings([]vnc.Encoding) error
}

type displayCalls struct {
	create    func(string) (purego.ID, error)
	port      func(purego.ID) uint16
	stop      func(purego.ID)
	dial      func(context.Context, string) (net.Conn, error)
	connect   func(net.Conn, *vnc.ClientConfig) (displayClient, int, int, error)
	recognize func(image.Image) (macsetup.Screen, error)
	wait      func(context.Context, time.Duration) error
}

func nativeDisplayCalls(m *appleMachine) *displayCalls {
	return &displayCalls{
		create: func(password string) (purego.ID, error) { return createVNC(m, password) },
		port: func(server purego.ID) (port uint16) {
			mainthread.Do(
				func() { port = purego.Send[uint16](server, purego.RegisterName("port")) },
			)
			return
		},
		stop: func(server purego.ID) {
			mainthread.Do(
				func() { server.Send(purego.RegisterName("stop")); server.Send(purego.RegisterName("release")) },
			)
		},
		dial: func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
		},
		connect: func(conn net.Conn, config *vnc.ClientConfig) (displayClient, int, int, error) {
			client, err := vnc.Client(conn, config)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("VNC handshake: %w", err)
			}
			return client, int(client.FrameBufferWidth), int(client.FrameBufferHeight), nil
		},
		recognize: recognize, wait: pause,
	}
}

func (m *appleMachine) screen(ctx context.Context) (macsetup.Screen, error) {
	if m.display == nil {
		d, err := m.openDisplay(ctx)
		if err != nil {
			return macsetup.Screen{}, err
		}
		m.display = d
	}
	frame, err := m.display.capture(ctx)
	if err != nil {
		return macsetup.Screen{}, err
	}
	screen, err := m.display.calls.recognize(frame)
	if err == nil {
		screen.Checks = macsetup.CheckboxStates(frame, screen)
	}
	return screen, err
}

func (m *appleMachine) input(ctx context.Context, actions []macsetup.Action) error {
	if m.display == nil {
		return fmt.Errorf("%w: display is not connected", ErrPrepared)
	}
	for _, action := range actions {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("guest display: %w", err)
		}
		if err := applyInput(m.display.client, action); err != nil {
			return fmt.Errorf("guest display: %w", err)
		}
		if err := m.display.calls.wait(ctx, 150*time.Millisecond); err != nil {
			return fmt.Errorf("guest display: %w", err)
		}
	}
	return nil
}

func createVNC(m *appleMachine, password string) (purego.ID, error) {
	return createVNCWith(m, password, purego.GetClass)
}

func createVNCWith(
	m *appleMachine,
	password string,
	lookup func(string) purego.Class,
) (purego.ID, error) {
	serverClass := lookup("_VZVNCServer")
	securityClass := lookup("_VZVNCAuthenticationSecurityConfiguration")
	if serverClass == 0 || securityClass == 0 {
		return 0, fmt.Errorf("%w: Apple VNC adapter unavailable", ErrPrepared)
	}
	var server purego.ID
	mainthread.Do(func() {
		security := purego.ID(securityClass).
			Send(purego.RegisterName("alloc")).
			Send(purego.RegisterName("initWithPassword:"), purego.NSString(password))
		server = purego.ID(serverClass).
			Send(purego.RegisterName("alloc")).
			Send(purego.RegisterName("initWithPort:queue:securityConfiguration:"), uint16(0), dispatch.GetGlobalQueue(0, 0).Ptr(), security)
		server.Send(purego.RegisterName("setVirtualMachine:"), obj.ID(m.vm))
		server.Send(purego.RegisterName("start"))
		security.Send(purego.RegisterName("release"))
	})
	return server, nil
}

func (m *appleMachine) openDisplay(ctx context.Context) (*appleDisplay, error) {
	calls := m.displayCalls
	if calls == nil {
		calls = nativeDisplayCalls(m)
	}
	d := &appleDisplay{calls: calls, messages: make(chan vnc.ServerMessage, 16)}
	password := rand.Text()[:8]
	var err error
	d.server, err = calls.create(password)
	if err != nil {
		return nil, err
	}
	if d.server == 0 {
		return nil, fmt.Errorf("%w: Apple VNC server creation failed", ErrPrepared)
	}
	var port uint16
	for port == 0 {
		port = calls.port(d.server)
		if err := calls.wait(ctx, 50*time.Millisecond); err != nil {
			d.close()
			return nil, err
		}
	}
	conn, err := calls.dial(ctx, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		d.close()
		return nil, fmt.Errorf("connect Apple display: %w", err)
	}
	d.conn = conn
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var width, height int
	d.client, width, height, err = calls.connect(
		conn,
		&vnc.ClientConfig{
			Auth:            []vnc.ClientAuth{&vnc.PasswordAuth{Password: password}},
			ServerMessageCh: d.messages,
		},
	)
	if err == nil {
		err = d.client.SetPixelFormat(
			&vnc.PixelFormat{
				BPP:        32,
				Depth:      24,
				TrueColor:  true,
				RedMax:     255,
				GreenMax:   255,
				BlueMax:    255,
				RedShift:   16,
				GreenShift: 8,
				BlueShift:  0,
			},
		)
	}
	if err == nil {
		err = d.client.SetEncodings([]vnc.Encoding{&vnc.RawEncoding{}})
	}
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		d.close()
		return nil, fmt.Errorf("initialize Apple display: %w", err)
	}
	if width < 1 || height < 1 || width > 65535 || height > 65535 ||
		int64(width)*int64(height) > 16*1024*1024 {
		d.close()
		return nil, fmt.Errorf("%w: invalid display dimensions", ErrPrepared)
	}
	d.frame = image.NewRGBA(
		image.Rect(0, 0, width, height),
	)
	return d, nil
}

func (d *appleDisplay) close() {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	if d.server != 0 {
		d.calls.stop(d.server)
		d.server = 0
	}
}

func (d *appleDisplay) capture(ctx context.Context) (*image.RGBA, error) {
	width, height := d.frame.Bounds().Dx(), d.frame.Bounds().Dy()
	if width < 1 || height < 1 || width > 65535 || height > 65535 {
		return nil, fmt.Errorf("%w: invalid display dimensions", ErrPrepared)
	}
	if err := d.client.FramebufferUpdateRequest(
		false,
		0,
		0,
		uint16(width),
		uint16(height),
	); err != nil {
		return nil, fmt.Errorf("request framebuffer: %w", err)
	}
	return d.captureWait(ctx, time.After(20*time.Second))
}

func (d *appleDisplay) captureWait(
	ctx context.Context,
	timeout <-chan time.Time,
) (*image.RGBA, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("macOS operation: %w", ctx.Err())
		case <-timeout:
			return nil, fmt.Errorf("%w: framebuffer timed out", ErrPrepared)
		case message, open := <-d.messages:
			if !open {
				return nil, fmt.Errorf("%w: display connection closed", ErrPrepared)
			}
			update, ok := message.(*vnc.FramebufferUpdateMessage)
			if !ok {
				continue
			}
			for _, r := range update.Rectangles {
				raw, ok := r.Enc.(*vnc.RawEncoding)
				if !ok {
					return nil, fmt.Errorf("%w: unexpected display encoding", ErrPrepared)
				}
				if r.Width == 0 || r.Height == 0 || len(raw.Colors) != int(r.Width)*int(r.Height) ||
					int(r.X)+int(r.Width) > d.frame.Bounds().Dx() ||
					int(r.Y)+int(r.Height) > d.frame.Bounds().Dy() {
					return nil, fmt.Errorf("%w: invalid framebuffer rectangle", ErrPrepared)
				}
				for i, pixel := range raw.Colors {
					x, y := int(r.X)+i%int(r.Width), int(r.Y)+i/int(r.Width)
					d.frame.SetRGBA(
						x,
						y,
						color.RGBA{
							R: uint8(pixel.R & 255),
							G: uint8(pixel.G & 255),
							B: uint8(pixel.B & 255),
							A: 255,
						},
					)
				}
			}
			return d.frame, nil
		}
	}
}

func recognize(frame image.Image) (macsetup.Screen, error) {
	screen := macsetup.Screen{Width: frame.Bounds().Dx(), Height: frame.Bounds().Dy()}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		return screen, fmt.Errorf("encode guest screen: %w", err)
	}
	request := vision.NewRecognizeTextRequest().
		WithRecognitionLevel(vision.RequestTextRecognitionLevelFast).
		WithUsesLanguageCorrection(false).
		WithRevision(3)
	defer request.Release()
	handler := vision.NewImageRequestHandlerWithDataOptions(encoded.Bytes(), nil)
	defer handler.Release()
	base, ok := obj.As(request, "VNRequest", vision.RequestFromID)
	if !ok {
		return screen, fmt.Errorf("%w: Vision request unavailable", ErrPrepared)
	}
	if err := handler.PerformRequests([]*vision.Request{base}); err != nil {
		return screen, fmt.Errorf("read guest screen: %w", err)
	}
	for _, observation := range base.Results() {
		text, ok := obj.As(
			observation,
			"VNRecognizedTextObservation",
			vision.RecognizedTextObservationFromID,
		)
		if !ok {
			continue
		}
		candidates := text.TopCandidates(1)
		if len(candidates) == 0 || candidates[0].Confidence() < 0.3 {
			continue
		}
		detected, ok := obj.As(
			observation,
			"VNDetectedObjectObservation",
			vision.DetectedObjectObservationFromID,
		)
		if !ok {
			continue
		}
		box := detected.BoundingBox()
		screen.Text = append(
			screen.Text,
			macsetup.Text{
				Value:  candidates[0].String(),
				X:      int(box.Origin.X * float64(screen.Width)),
				Y:      int((1 - box.Origin.Y - box.Size.Height) * float64(screen.Height)),
				Width:  int(box.Size.Width * float64(screen.Width)),
				Height: int(box.Size.Height * float64(screen.Height)),
			},
		)
	}
	return screen, nil
}

// The protocol maps Command to Alt_L on Apple's VNC server.
var inputKeys = map[string]uint32{
	"cmd":       0xffe9,
	"ctrl":      0xffe3,
	"shift":     0xffe1,
	"alt":       0xffe7,
	"enter":     0xff0d,
	"tab":       0xff09,
	"escape":    0xff1b,
	"esc":       0xff1b,
	"space":     32,
	"up":        0xff52,
	"down":      0xff54,
	"left":      0xff51,
	"right":     0xff53,
	"backspace": 0xff08,
}

type inputClient interface {
	KeyEvent(uint32, bool) error
	PointerEvent(vnc.ButtonMask, uint16, uint16) error
}

func applyInput(c inputClient, a macsetup.Action) error {
	if a.Kind == "click" {
		if a.X < 0 || a.X > 65535 || a.Y < 0 || a.Y > 65535 {
			return fmt.Errorf("%w: invalid click", ErrPrepared)
		}
		if err := c.PointerEvent(1, uint16(a.X), uint16(a.Y)); err != nil {
			return fmt.Errorf("guest click: %w", err)
		}
		if err := c.PointerEvent(0, uint16(a.X), uint16(a.Y)); err != nil {
			return fmt.Errorf("guest click release: %w", err)
		}
		return nil
	}
	if a.Kind == "type" {
		for _, r := range a.Value {
			if r < 32 || r > 126 {
				return fmt.Errorf("%w: unsupported guest input character", ErrPrepared)
			}
			key := uint32(r)
			shift := false
			if r >= 'A' && r <= 'Z' {
				key = uint32(r + 32)
				shift = true
			}
			shifted := "!@#$%^&*()_+{}|:\"<>?~"
			normal := "1234567890-=[]\\;',./`"
			if i := strings.IndexRune(shifted, r); i >= 0 {
				key = uint32(normal[i])
				shift = true
			}
			keys := []uint32{key}
			if shift {
				keys = []uint32{inputKeys["shift"], key}
			}
			if err := pressKeys(c, keys); err != nil {
				return err
			}
		}
		return nil
	}
	if a.Kind != "key" {
		return fmt.Errorf("%w: unknown guest input", ErrPrepared)
	}
	var keys []uint32
	for _, part := range strings.Split(a.Value, "+") {
		key, ok := inputKeys[part]
		if !ok && len(part) == 1 {
			key = uint32(part[0])
			ok = true
		}
		if !ok {
			return fmt.Errorf("%w: unknown guest key", ErrPrepared)
		}
		keys = append(keys, key)
	}
	return pressKeys(c, keys)
}

func pressKeys(c inputClient, keys []uint32) error {
	for _, key := range keys {
		if err := c.KeyEvent(key, true); err != nil {
			return fmt.Errorf("guest key: %w", err)
		}
	}
	for i := len(keys) - 1; i >= 0; i-- {
		if err := c.KeyEvent(keys[i], false); err != nil {
			return fmt.Errorf("guest key release: %w", err)
		}
	}
	return nil
}
