package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/and-elf/omm/internal/models"
)

type statusResponse struct {
	Status string `json:"status"`
}

type profileResponse struct {
	Profile models.Profile `json:"profile"`
}

func TestIntegrationHealth(t *testing.T) {
	h := New(t, Options{})
	defer h.Close()

	resp, err := h.Get("/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected ok status, got %q", body.Status)
	}
}

func TestIntegrationCreateAndGetHome(t *testing.T) {
	h := New(t, Options{})
	defer h.Close()

	home := models.Home{ID: "home-1", Name: "Test Home", Controller: "gw1"}
	resp, err := h.PostJSON("/homes", home)
	if err != nil {
		t.Fatalf("create home failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}

	resp, err = h.Get("/homes/home-1")
	if err != nil {
		t.Fatalf("get home failed: %v", err)
	}
	defer resp.Body.Close()

	var got models.Home
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode home: %v", err)
	}
	if got.ID != home.ID || got.Name != home.Name || got.Controller != home.Controller {
		t.Fatalf("home mismatch: got %+v", got)
	}
}

func TestIntegrationUDPReceive(t *testing.T) {
	h := New(t, Options{})
	defer h.Close()

	payload := []byte("ping")
	if err := h.SendUDP(payload); err != nil {
		t.Fatalf("send udp failed: %v", err)
	}

	received, err := h.WaitForUDP(2 * time.Second)
	if err != nil {
		t.Fatalf("udp packet not received: %v", err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatalf("expected udp payload %q, got %q", payload, received)
	}
}
