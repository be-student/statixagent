package agent

import (
	"net"
	"net/http"
	"time"

	"github.com/eliau2005/statixagent/internal/services"
)

// httpClient is shared by HTTP healthchecks.
var httpClient = &http.Client{Timeout: 15 * time.Second}

func defaultDialer() services.Dialer {
	var d net.Dialer
	return d.DialContext
}
