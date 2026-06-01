package topology

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// CommandRunner runs an external command and returns its combined output. It is
// injected so the batman source can be tested without batctl present.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs commands via os/exec.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// BatctlMesh reads mesh neighbors from `batctl o` (originators). batman-adv has
// no widely-adopted Go binding, so this is the documented MVP approach,
// encapsulated entirely within this package.
type BatctlMesh struct {
	Run       CommandRunner
	Interface string // batman interface, e.g. "bat0" (optional)
}

func (b BatctlMesh) Neighbors(ctx context.Context) ([]Neighbor, error) {
	run := b.Run
	if run == nil {
		run = ExecRunner
	}
	args := []string{}
	if b.Interface != "" {
		args = append(args, "-m", b.Interface)
	}
	args = append(args, "o")

	out, err := run(ctx, "batctl", args...)
	if err != nil {
		return nil, err
	}
	return parseOriginators(out), nil
}

var (
	macRe = regexp.MustCompile(`([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}`)
	tqRe  = regexp.MustCompile(`\((\s*\d+)\)`)
)

// parseOriginators parses `batctl o` output. Each originator line carries the
// originator MAC, a "(TQ)" transmit quality, and a nexthop MAC. Lines marked
// with "*" are the currently-selected route. We keep the best TQ seen per
// originator.
func parseOriginators(out []byte) []Neighbor {
	best := map[string]int{}
	order := []string{}

	for _, line := range strings.Split(string(out), "\n") {
		macs := macRe.FindAllString(line, -1)
		tq := tqRe.FindStringSubmatch(line)
		if len(macs) == 0 || tq == nil {
			continue
		}
		originator := strings.ToLower(macs[0])
		quality, err := strconv.Atoi(strings.TrimSpace(tq[1]))
		if err != nil {
			continue
		}
		if cur, ok := best[originator]; !ok || quality > cur {
			if !ok {
				order = append(order, originator)
			}
			best[originator] = quality
		}
	}

	neighbors := make([]Neighbor, 0, len(order))
	for _, mac := range order {
		neighbors = append(neighbors, Neighbor{ID: mac, TQ: best[mac]})
	}
	return neighbors
}
