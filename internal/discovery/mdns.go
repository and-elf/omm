package discovery

import (
	"context"
	"log"

	"github.com/grandcat/zeroconf"
)

type ServiceEntry = zeroconf.ServiceEntry

type MDNSServiceEntryHandler func(entry *ServiceEntry)

func DiscoverMeshControllers(ctx context.Context, service, domain string, handler MDNSServiceEntryHandler) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			handler(entry)
		}
	}()

	if err := resolver.Browse(ctx, service, domain, entries); err != nil {
		close(entries)
		return err
	}

	<-ctx.Done()
	log.Println("mDNS discovery stopped")
	return nil
}
