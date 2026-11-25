package netcfg

import (
	"strconv"
	"strings"

	"github.com/saeedshamc/vpnctl/internal/state"
)

func parseDefaultRoute(line string) state.RouteSnapshot {
	// e.g. "default via 192.168.1.1 dev wlan0 proto dhcp metric 600"
	fields := strings.Fields(line)
	r := state.RouteSnapshot{Dst: "default", Family: "inet"}
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				r.Gateway = fields[i+1]
				i++
			}
		case "dev":
			if i+1 < len(fields) {
				r.Dev = fields[i+1]
				i++
			}
		case "metric":
			if i+1 < len(fields) {
				r.Metric, _ = strconv.Atoi(fields[i+1])
				i++
			}
		}
	}
	return r
}
