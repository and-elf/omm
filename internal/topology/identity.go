package topology

import (
	"context"
	"os"
	"strings"
)

// SysfsSelfAddrs reports this node's batman-adv address — the MAC of its bat0
// interface, which is how peers list it as an originator. Read from
// `/sys/class/net/<iface>/address`; an unreadable interface yields no addresses
// (the node simply can't be reconciled, same as before).
type SysfsSelfAddrs struct {
	Iface string     // batman interface, e.g. "bat0"
	Read  fileReader // nil => os.ReadFile
}

func (s SysfsSelfAddrs) SelfAddrs(ctx context.Context) []string {
	if s.Iface == "" {
		return nil
	}
	read := s.Read
	if read == nil {
		read = os.ReadFile
	}
	b, err := read("/sys/class/net/" + s.Iface + "/address")
	if err != nil {
		return nil
	}
	mac := strings.ToLower(strings.TrimSpace(string(b)))
	if mac == "" {
		return nil
	}
	return []string{mac}
}
