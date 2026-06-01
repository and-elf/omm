package profiles

import (
	"context"
	"fmt"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
	"github.com/and-elf/omm/internal/uci"
)

type ProfileManager interface {
	ApplyProfile(ctx context.Context, profile models.Profile) error
	ApplyProfileForHome(ctx context.Context, homeID string) error
}

type Manager struct {
	store     storage.Store
	uciClient uci.Client
}

func NewManager(store storage.Store, uciClient uci.Client) ProfileManager {
	return &Manager{store: store, uciClient: uciClient}
}

func (m *Manager) ApplyProfile(ctx context.Context, profile models.Profile) error {
	if profile.MeshSSID != "" {
		if err := m.uciClient.Set(ctx, "wireless", "mesh", "ssid", profile.MeshSSID); err != nil {
			return fmt.Errorf("set mesh ssid: %w", err)
		}
	}

	if profile.MeshKey != "" {
		if err := m.uciClient.Set(ctx, "wireless", "mesh", "key", profile.MeshKey); err != nil {
			return fmt.Errorf("set mesh key: %w", err)
		}
	}

	if profile.NodeName != "" {
		if err := m.uciClient.Set(ctx, "system", "@system[0]", "hostname", profile.NodeName); err != nil {
			return fmt.Errorf("set hostname: %w", err)
		}
	}

	if err := m.uciClient.Commit(ctx, "wireless"); err != nil {
		return fmt.Errorf("commit wireless: %w", err)
	}

	if err := m.uciClient.Commit(ctx, "system"); err != nil {
		return fmt.Errorf("commit system: %w", err)
	}

	return nil
}

func (m *Manager) ApplyProfileForHome(ctx context.Context, homeID string) error {
	profile, err := m.store.GetProfile(ctx, homeID)
	if err != nil {
		return err
	}
	return m.ApplyProfile(ctx, profile)
}
