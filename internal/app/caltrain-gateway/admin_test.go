package caltraingateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// renderTestPage renders a content template through the shared layout and
// returns the response body.
func renderTestPage(t *testing.T, name, contentHTML string, page adminPage) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	renderAdminPage(recorder, name, contentHTML, page)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestAdminLayoutRendersTabs checks that every admin page gets the same tab bar
// and that exactly the current section is marked active.
func TestAdminLayoutRendersTabs(t *testing.T) {
	content := `{{define "content"}}<p>body</p>{{end}}`

	for _, tab := range adminTabs {
		t.Run(tab.ID, func(t *testing.T) {
			body := renderTestPage(t, "probe", content, newAdminPage(tab.ID, "Probe", nil))

			for _, other := range adminTabs {
				if !strings.Contains(body, `href="`+other.Href+`"`) {
					t.Errorf("expected a tab linking to %s", other.Href)
				}
				if !strings.Contains(body, other.Label) {
					t.Errorf("expected tab label %q", other.Label)
				}
			}
			if got := strings.Count(body, `class="active"`); got != 1 {
				t.Errorf("expected exactly 1 active tab, got %d", got)
			}
			active := `<a href="` + tab.Href + `" class="active" aria-current="page">`
			if !strings.Contains(body, active) {
				t.Errorf("expected %s to be the active tab", tab.Href)
			}
			if !strings.Contains(body, "<p>body</p>") {
				t.Error("expected the content block to be rendered inside the layout")
			}
		})
	}
}

// TestAdminLayoutReportsBrokenTemplate ensures a malformed content template
// fails loudly instead of emitting a half-rendered page.
func TestAdminLayoutReportsBrokenTemplate(t *testing.T) {
	recorder := httptest.NewRecorder()
	renderAdminPage(recorder, "broken", `{{define "content"}}{{.Missing`, newAdminPage(tabSupport, "Broken", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for an unparseable template, got %d", recorder.Code)
	}
}

// TestAdminContentTemplatesRender renders each real content template through the
// layout, which catches composition mistakes that leave a page blank.
func TestAdminContentTemplatesRender(t *testing.T) {
	departures := buildDeparturesPage(nil, DepartureFilter{}, 1, 0)

	tests := []struct {
		name     string
		tab      string
		content  string
		data     any
		expected string
	}{
		{"support list", tabSupport, supportListHTML,
			struct{ Requests []SupportRequestRow }{}, "No support requests found"},
		{"support detail", tabSupport, supportDetailHTML,
			&SupportRequestRow{ID: 7, Name: "Ada", Message: "hello"}, "Support request #7"},
		{"service alerts list", tabServiceAlerts, serviceAlertsListHTML,
			struct{ Alerts []ServiceAlertRow }{}, "No service alerts found"},
		{"service alerts detail", tabServiceAlerts, serviceAlertsDetailHTML,
			serviceAlertDetailView{ServiceAlertRow: &ServiceAlertRow{ID: 3, EntityID: "alert-3"}}, "Service alert #3"},
		{"departures list", tabDepartures, departuresListHTML,
			departures, "No departures recorded yet"},
		{"departures detail", tabDepartures, departuresDetailHTML,
			departureView{TrainDepartureRow: &TrainDepartureRow{TrainNumber: "401", StopID: "70021"}}, "Train 401"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderTestPage(t, tt.name, tt.content, newAdminPage(tt.tab, tt.name, tt.data))

			if !strings.Contains(body, tt.expected) {
				t.Errorf("expected body to contain %q", tt.expected)
			}
			if !strings.Contains(body, `class="tabs"`) {
				t.Error("expected the shared tab bar")
			}
			if strings.Count(body, `class="active"`) != 1 {
				t.Error("expected exactly one active tab")
			}
		})
	}
}

// TestAdminIndexHandler verifies the admin root redirects to the first tab and
// that unknown subtree paths are still rejected.
func TestAdminIndexHandler(t *testing.T) {
	tests := []struct {
		path         string
		wantStatus   int
		wantLocation string
	}{
		{"/admin/", http.StatusFound, adminTabs[0].Href},
		{"/admin", http.StatusFound, adminTabs[0].Href},
		{"/admin/nonsense", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			adminIndexHandler(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d", tt.wantStatus, recorder.Code)
			}
			if tt.wantLocation != "" {
				if got := recorder.Header().Get("Location"); got != tt.wantLocation {
					t.Errorf("expected redirect to %s, got %s", tt.wantLocation, got)
				}
			}
		})
	}
}
