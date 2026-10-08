//go:build darwin && arm64

package macos

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/purego"
	vnc "github.com/mitchellh/go-vnc"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/weaveplatform/weaveplatform-oci/pkg/macsetup"
)

type displayRecorder struct {
	keys          []uint32
	down          []bool
	buttons       []vnc.ButtonMask
	fail          string
	count, failAt int
}

func (c *displayRecorder) KeyEvent(k uint32, down bool) error {
	c.keys = append(c.keys, k)
	c.down = append(c.down, down)
	c.count++
	if c.count == c.failAt {
		return ErrPrepared
	}
	return nil
}

func (c *displayRecorder) PointerEvent(b vnc.ButtonMask, _, _ uint16) error {
	c.buttons = append(c.buttons, b)
	c.count++
	if c.count == c.failAt {
		return ErrPrepared
	}
	return nil
}

func (c *displayRecorder) FramebufferUpdateRequest(bool, uint16, uint16, uint16, uint16) error {
	if c.fail == "frame" {
		return ErrPrepared
	}
	return nil
}

func (c *displayRecorder) SetPixelFormat(f *vnc.PixelFormat) error {
	if c.fail == "format" {
		return ErrPrepared
	}
	if f.BPP != 32 || f.Depth != 24 {
		return ErrPrepared
	}
	return nil
}

func (c *displayRecorder) SetEncodings(e []vnc.Encoding) error {
	if c.fail == "encoding" {
		return ErrPrepared
	}
	if len(e) != 1 {
		return ErrPrepared
	}
	return nil
}

func TestDisplayInput(t *testing.T) {
	c := &displayRecorder{}
	must(t, applyInput(c, macsetup.Action{Kind: "key", Value: "cmd+a"}))
	if !reflect.DeepEqual(c.keys, []uint32{0xffe9, 'a', 'a', 0xffe9}) ||
		!reflect.DeepEqual(c.down, []bool{true, true, false, false}) {
		t.Fatal(c)
	}
	c = &displayRecorder{}
	must(t, applyInput(c, macsetup.Action{Kind: "type", Value: "aA! "}))
	if !reflect.DeepEqual(
		c.keys,
		[]uint32{'a', 'a', 0xffe1, 'a', 'a', 0xffe1, 0xffe1, '1', '1', 0xffe1, ' ', ' '},
	) {
		t.Fatal(c.keys)
	}
	c = &displayRecorder{}
	must(t, applyInput(c, macsetup.Action{Kind: "click", X: 3, Y: 7}))
	if !reflect.DeepEqual(c.buttons, []vnc.ButtonMask{1, 0}) {
		t.Fatal(c.buttons)
	}
	for _, a := range []macsetup.Action{{Kind: "click", X: -1}, {Kind: "click", Y: 65536}, {Kind: "type", Value: "€"}, {Kind: "other"}, {Kind: "key", Value: "unknown"}} {
		if applyInput(&displayRecorder{}, a) == nil {
			t.Fatal(a)
		}
	}
	for _, a := range []macsetup.Action{{Kind: "click"}, {Kind: "type", Value: "a"}, {Kind: "key", Value: "enter"}} {
		for _, failAt := range []int{1, 2} {
			if applyInput(&displayRecorder{failAt: failAt}, a) == nil {
				t.Fatal(a, failAt)
			}
		}
	}
}

func rectangle() *vnc.FramebufferUpdateMessage {
	return &vnc.FramebufferUpdateMessage{
		Rectangles: []vnc.Rectangle{
			{
				X:      1,
				Y:      1,
				Width:  1,
				Height: 1,
				Enc:    &vnc.RawEncoding{Colors: []vnc.Color{{R: 255, G: 32, B: 8}}},
			},
		},
	}
}

func TestFrameCapture(t *testing.T) {
	for _, mode := range []string{"success", "request", "dimensions", "closed", "cancelled", "timeout", "encoding", "bounds", "empty"} {
		t.Run(mode, func(t *testing.T) {
			c := &displayRecorder{}
			d := &appleDisplay{
				client:   c,
				messages: make(chan vnc.ServerMessage, 2),
				frame:    image.NewRGBA(image.Rect(0, 0, 4, 4)),
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timeout := make(chan time.Time, 1)
			update := rectangle()
			switch mode {
			case "success":
				d.messages <- new(vnc.BellMessage)
				d.messages <- update
			case "dimensions":
				d.frame = image.NewRGBA(image.Rectangle{})
			case "request":
				c.fail = "frame"
			case "closed":
				close(d.messages)
			case "cancelled":
				cancel()
			case "timeout":
				timeout <- time.Now()
			case "encoding":
				update.Rectangles[0].Enc = nil
				d.messages <- update
			case "bounds":
				update.Rectangles[0].Width = 0
				d.messages <- update
			case "empty":
				update.Rectangles[0].Enc = &vnc.RawEncoding{}
				d.messages <- update
			}
			var frame *image.RGBA
			var err error
			if mode == "request" || mode == "success" || mode == "dimensions" {
				frame, err = d.capture(ctx)
			} else {
				frame, err = d.captureWait(ctx, timeout)
			}
			if mode != "success" {
				if err == nil {
					t.Fatal("bad frame accepted")
				}
				return
			}
			must(t, err)
			if frame.RGBAAt(1, 1) != (color.RGBA{255, 32, 8, 255}) {
				t.Fatal(frame.RGBAAt(1, 1))
			}
		})
	}
}

func TestDisplayConnectionAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "create", "nil server", "port", "dial", "handshake", "dimensions", "format", "encoding", "frame", "recognize", "input", "input cancelled", "input wait"} {
		t.Run(mode, func(t *testing.T) {
			c := &displayRecorder{fail: mode}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			stopped := false
			calls := &displayCalls{
				create: func(password string) (purego.ID, error) {
					if len(password) != 8 {
						t.Fatal(password)
					}
					if mode == "create" {
						return 0, ErrPrepared
					}
					if mode == "nil server" {
						return 0, nil
					}
					return 1, nil
				},
				port: func(purego.ID) uint16 { return 5901 },
				stop: func(purego.ID) { stopped = true },
				dial: func(_ context.Context, address string) (net.Conn, error) {
					if address != "127.0.0.1:5901" {
						t.Fatal(address)
					}
					if mode == "dial" {
						return nil, ErrPrepared
					}
					return a, nil
				},
				connect: func(_ net.Conn, config *vnc.ClientConfig) (displayClient, int, int, error) {
					if len(config.Auth) != 1 {
						t.Fatal("unauthenticated VNC")
					}
					if mode == "handshake" {
						return nil, 0, 0, ErrPrepared
					}
					if mode == "dimensions" {
						return c, 0, 4, nil
					}
					config.ServerMessageCh <- rectangle()
					return c, 4, 4, nil
				},
				recognize: func(image.Image) (macsetup.Screen, error) {
					if mode == "recognize" {
						return macsetup.Screen{}, ErrPrepared
					}
					return macsetup.Screen{Width: 4, Height: 4}, nil
				},
				wait: func(_ context.Context, d time.Duration) error {
					if mode == "port" || mode == "input wait" && d == 150*time.Millisecond {
						return ErrPrepared
					}
					return nil
				},
			}
			m := &appleMachine{displayCalls: calls}
			s, err := m.screen(t.Context())
			switch mode {
			case "create",
				"nil server",
				"port",
				"dial",
				"handshake",
				"dimensions",
				"format",
				"encoding",
				"frame",
				"recognize":
				if err == nil {
					t.Fatal("connection failed open")
				}
				if mode != "create" && mode != "nil server" && mode != "frame" &&
					mode != "recognize" &&
					!stopped {
					t.Fatal("leaked VNC server")
				}
				if m.display != nil {
					m.display.close()
				}
				return
			}
			must(t, err)
			if s.Width != 4 {
				t.Fatal(s)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "input cancelled" {
				cancel()
			}
			if mode == "input" {
				c.failAt = 1
			}
			err = m.input(ctx, []macsetup.Action{{Kind: "key", Value: "enter"}})
			if mode == "success" {
				must(t, err)
			} else if err == nil {
				t.Fatal("input failed open")
			}
			m.display.close()
			m.display.close()
			if !stopped {
				t.Fatal("leaked server")
			}
		})
	}
	m := &appleMachine{}
	if m.input(t.Context(), nil) == nil {
		t.Fatal("input without display")
	}
	calls := nativeDisplayCalls(m)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := calls.dial(ctx, "127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	b.Close()
	defer a.Close()
	if _, _, _, err := calls.connect(a, &vnc.ClientConfig{}); err == nil {
		t.Fatal("closed handshake accepted")
	}
}

func TestVisionTextAndCoordinates(t *testing.T) {
	small := image.NewRGBA(image.Rect(0, 0, 220, 45))
	draw.Draw(small, small.Bounds(), image.White, image.Point{}, draw.Src)
	d := font.Drawer{Dst: small, Src: image.Black, Face: basicfont.Face7x13, Dot: fixed.P(12, 25)}
	d.DrawString("Welcome to macOS")
	frame := image.NewRGBA(image.Rect(0, 0, 880, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 880; x++ {
			frame.Set(x, y, small.At(x/4, y/4))
		}
	}
	screen, err := recognize(frame)
	must(t, err)
	found := false
	for _, word := range screen.Text {
		if strings.Contains(strings.ToLower(word.Value), "macos") {
			found = true
		}
		if word.X < 0 || word.Y < 0 || word.Width <= 0 || word.Height <= 0 {
			t.Fatal(word)
		}
	}
	if !found || screen.Width != 880 || screen.Height != 180 {
		t.Fatal(screen)
	}
	if _, err := recognize(image.NewRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty frame accepted")
	}
}
