package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// stubSignals implements SignalSource with canned RSSI.
type stubSignals map[string]int

func (s stubSignals) SignalByMAC(context.Context) (map[string]int, error) { return s, nil }

func TestHomeSelectionEndpoint(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	ctx := context.Background()
	// Self home plus two external homes with controller MACs.
	_ = store.CreateHome(ctx, models.Home{ID: "self", Name: "Self", BSSID: "00:00:00:00:00:01"})
	_ = store.CreateHome(ctx, models.Home{ID: "cottage", Name: "Cottage", BSSID: "aa:bb:cc:dd:ee:01"})
	_ = store.CreateHome(ctx, models.Home{ID: "parents", Name: "Parents", BSSID: "aa:bb:cc:dd:ee:02"})

	signals := stubSignals{"aa:bb:cc:dd:ee:01": -55, "aa:bb:cc:dd:ee:02": -80}
	router := NewRouter(store, noopProfileManager{}, WithSelfHome("self"), WithSignalSource(signals))

	rw := doGet(t, router, "/home-selection")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	var resp homeSelectionResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// External home with the strongest RSSI wins; self is a last resort.
	if resp.RecommendedHomeID != "cottage" {
		t.Fatalf("expected cottage recommended, got %q", resp.RecommendedHomeID)
	}
	// Candidates carry the live signal mapped by controller MAC.
	byID := map[string]int{}
	for _, c := range resp.Candidates {
		byID[c.HomeID] = c.Signal
	}
	if byID["cottage"] != -55 || byID["parents"] != -80 {
		t.Fatalf("signals not surfaced: %+v", resp.Candidates)
	}
}
