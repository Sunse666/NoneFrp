package client

import (
	"sort"

	"github.com/Sunse666/NoneFrp/internal/netinfo"
)

// directEndpoints lists the addresses a player might dial instead of going
// through the relay. The relay publishes them so a joining client can try the
// short path first.
func directEndpoints(port int) []string {
	candidates := netinfo.Candidates()

	// A player somewhere else on the internet prefers public addresses; a
	// player on the same LAN falls through to the private ones quickly.
	rank := func(kind netinfo.Kind) int {
		switch kind {
		case netinfo.KindPublic:
			return 0
		case netinfo.KindTailnet:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return rank(candidates[i].Kind) < rank(candidates[j].Kind)
	})

	endpoints := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		endpoints = append(endpoints, netinfo.Endpoint(candidate.IP, port))
	}
	return endpoints
}
