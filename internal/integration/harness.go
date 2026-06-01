package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/profiles"
	"github.com/and-elf/omm/internal/storage"
)

type Options struct {
	HTTPAddr       string
	UDPAddr        string
	Store          storage.Store
	ProfileManager profiles.ProfileManager
	ReadyTimeout   time.Duration
}

type Harness struct {
	t            *testing.T
	httpListener net.Listener
	httpAddr     string
	udpConn      net.PacketConn
	udpAddr      *net.UDPAddr
	httpClient   *http.Client
	server       *http.Server
	udpReceived  chan []byte
	readyTimeout time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	cleanup      func()
}

type noopProfileManager struct{}

func (noopProfileManager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	return nil
}

func (noopProfileManager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	return nil
}

func New(t *testing.T, opts Options) *Harness {
	t.Helper()

	if opts.HTTPAddr == "" {
		opts.HTTPAddr = "127.0.0.1:0"
	}
	if opts.UDPAddr == "" {
		opts.UDPAddr = "127.0.0.1:0"
	}
	if opts.ReadyTimeout == 0 {
		opts.ReadyTimeout = 5 * time.Second
	}
	if opts.ProfileManager == nil {
		opts.ProfileManager = noopProfileManager{}
	}

	var cleanup func()
	if opts.Store == nil {
		db, err := storage.OpenDB(":memory:")
		if err != nil {
			t.Fatalf("open in-memory database: %v", err)
		}
		opts.Store = storage.NewStore(db)
		cleanup = func() {
			db.Close()
		}
	}

	router := api.NewRouter(opts.Store, opts.ProfileManager)

	httpListener, err := net.Listen("tcp", opts.HTTPAddr)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatalf("listen http: %v", err)
	}

	udpConn, err := net.ListenPacket("udp4", opts.UDPAddr)
	if err != nil {
		httpListener.Close()
		if cleanup != nil {
			cleanup()
		}
		t.Fatalf("listen udp: %v", err)
	}

	localUDPAddr, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		httpListener.Close()
		udpConn.Close()
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("udp local address is not a UDP address")
	}

	h := &Harness{
		t:            t,
		httpListener: httpListener,
		httpAddr:     httpListener.Addr().String(),
		udpConn:      udpConn,
		udpAddr:      localUDPAddr,
		httpClient: &http.Client{
			Timeout: opts.ReadyTimeout,
		},
		server:       &http.Server{Handler: router},
		udpReceived:  make(chan []byte, 16),
		readyTimeout: opts.ReadyTimeout,
		cleanup:      cleanup,
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		if err := h.server.Serve(h.httpListener); err != nil && err != http.ErrServerClosed {
			t.Errorf("http serve: %v", err)
		}
	}()

	go func() {
		defer h.wg.Done()
		h.listenUDP(ctx)
	}()

	h.waitReady()

	return h
}

func (h *Harness) waitReady() {
	deadline := time.Now().Add(h.readyTimeout)
	url := h.URL() + "/health"

	for time.Now().Before(deadline) {
		resp, err := h.httpClient.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	h.t.Fatalf("server did not become ready within %s", h.readyTimeout)
}

func (h *Harness) listenUDP(ctx context.Context) {
	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, _, err := h.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				h.t.Errorf("udp read: %v", err)
				return
			}
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])
		select {
		case h.udpReceived <- packet:
		default:
		}
	}
}

func (h *Harness) Close() {
	h.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = h.server.Shutdown(ctx)
	_ = h.udpConn.Close()
	h.wg.Wait()
	if h.cleanup != nil {
		h.cleanup()
	}
}

func (h *Harness) URL() string {
	return "http://" + h.httpAddr
}

func (h *Harness) UDPAddress() string {
	return h.udpAddr.String()
}

func (h *Harness) Get(path string) (*http.Response, error) {
	return h.httpClient.Get(h.URL() + path)
}

func (h *Harness) GetJSON(path string, out interface{}) (*http.Response, error) {
	resp, err := h.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func (h *Harness) PostJSON(path string, payload interface{}) (*http.Response, error) {
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(payload); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, h.URL()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return h.httpClient.Do(req)
}

func (h *Harness) SendUDP(payload []byte) error {
	conn, err := net.DialUDP("udp4", nil, h.udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(payload)
	return err
}

func (h *Harness) WaitForUDP(timeout time.Duration) ([]byte, error) {
	select {
	case msg := <-h.udpReceived:
		return msg, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for udp packet")
	}
}
