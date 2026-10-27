package scan

import (
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
)

func TestAutoEnv(t *testing.T) {
	vars := []string{"MYAPP_BASE_PATH", "MYAPP_URL", "UPSTREAM_URL", "FRONTEND_URL", "DATA_LOCATION",
		"BACKUP_LOCATION", "DB_DATA_LOCATION", "_BASE_PATH", "TZ"}
	for _, tc := range []struct {
		routing string
		want    string
	}{
		{config.RoutingPath, "BACKUP_LOCATION=${HL_DATA}/backups DATA_LOCATION=${HL_DATA} MYAPP_BASE_PATH=${HL_PREFIX} MYAPP_URL=${HL_URL}"},
		{config.RoutingHost, "BACKUP_LOCATION=${HL_DATA}/backups DATA_LOCATION=${HL_DATA} MYAPP_URL=${HL_URL}"},
		{config.RoutingNone, "BACKUP_LOCATION=${HL_DATA}/backups DATA_LOCATION=${HL_DATA}"},
	} {
		if got := strings.Join(EnvList(AutoEnv(vars, tc.routing)), " "); got != tc.want {
			t.Errorf("%s: got  %s\nwant %s", tc.routing, got, tc.want)
		}
	}
	// A URL without its base path is someone else's URL.
	if got := AutoEnv([]string{"OTHER_URL", "TZ"}, config.RoutingPath); got != nil {
		t.Errorf("got %v, want nothing", got)
	}
}
