package caltraingateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveBuildVersion covers how the stamped and embedded metadata combine.
// The fallbacks matter: an unstamped build must identify itself as a
// development build rather than silently claiming to be a release.
func TestResolveBuildVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		revision string
		vcs      vcsInfo
		want     string
	}{
		{
			name:    "stamped release",
			version: "v1.5.0",
			want:    "v1.5.0",
		},
		{
			name:     "stamped release with revision",
			version:  "v1.5.0",
			revision: "b791e74fabc1234",
			want:     "v1.5.0 (b791e74)",
		},
		{
			name: "unstamped falls back to the embedded commit",
			vcs:  vcsInfo{revision: "b791e74fabc1234"},
			want: "dev (b791e74)",
		},
		{
			name: "dirty working tree is flagged",
			vcs:  vcsInfo{revision: "b791e74fabc1234", modified: true},
			want: "dev (b791e74-dirty)",
		},
		{
			name: "no metadata at all",
			want: "dev",
		},
		{
			name:    "describe output already carrying the commit is not repeated",
			version: "v1.5.0-2-gb791e74",
			vcs:     vcsInfo{revision: "b791e74fabc1234"},
			want:    "v1.5.0-2-gb791e74",
		},
		{
			name:    "dirty describe build flags dirt without repeating the commit",
			version: "v1.5.0-2-gb791e74",
			vcs:     vcsInfo{revision: "b791e74fabc1234", modified: true},
			want:    "v1.5.0-2-gb791e74 (dirty)",
		},
		{
			name: "dirty with no revision available",
			vcs:  vcsInfo{modified: true},
			want: "dev (dirty)",
		},
		{
			name:    "surrounding whitespace is trimmed",
			version: "  v1.5.0  ",
			want:    "v1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalVersion, originalRevision := buildVersion, buildRevision
			buildVersion, buildRevision = tt.version, tt.revision
			defer func() { buildVersion, buildRevision = originalVersion, originalRevision }()

			if got := resolveBuildVersion(tt.vcs); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestBuildVersionIsNeverEmpty guards the UI contract: the dashboard and admin
// header always render this value, so it must never be blank.
func TestBuildVersionIsNeverEmpty(t *testing.T) {
	if BuildVersion() == "" {
		t.Error("expected a non-empty build version")
	}
}

func TestUIStatsHandlerReportsVersion(t *testing.T) {
	recorder := httptest.NewRecorder()
	uiStatsHandler(recorder, httptest.NewRequest(http.MethodGet, "/ui/stats", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var payload struct {
		Version       string         `json:"version"`
		UptimeSeconds int64          `json:"uptime_seconds"`
		Endpoints     map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}
	if payload.Version == "" {
		t.Error("expected the stats payload to carry a version")
	}
	if payload.Version != BuildVersion() {
		t.Errorf("expected version %q, got %q", BuildVersion(), payload.Version)
	}
}

// TestAdminLayoutShowsVersion confirms the version reaches every admin page,
// since it is rendered by the shared layout rather than per page.
func TestAdminLayoutShowsVersion(t *testing.T) {
	body := renderTestPage(t, "probe", `{{define "content"}}<p>body</p>{{end}}`,
		newAdminPage(tabSupport, "Probe", nil))

	if !strings.Contains(body, BuildVersion()) {
		t.Errorf("expected the admin layout to show %q", BuildVersion())
	}
	if !strings.Contains(body, `class="version"`) {
		t.Error("expected the version to be rendered in the brand header")
	}
}
