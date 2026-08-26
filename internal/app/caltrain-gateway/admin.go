package caltraingateway

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed web/admin_layout.html
var adminLayoutHTML string

//go:embed web/admin_unavailable.html
var adminUnavailableHTML string

// adminUnavailableView explains why the admin section is switched off and how to
// switch it on.
type adminUnavailableView struct {
	Reason      string
	Explanation string
	Fix         string
	Example     string
}

// adminUnavailableHandler responds when the admin section is not configured.
//
// It is registered in place of the admin routes, which previously left /admin
// unrouted and therefore proxied upstream, producing a 401 that looked like a
// rejected login. The two causes need different fixes, so they are reported
// separately.
//
// The response is 503 rather than 404 because the section exists but is
// disabled by configuration. No basic auth is applied: there are no credentials
// to verify against, and the page exposes no data.
func adminUnavailableHandler(w http.ResponseWriter, r *http.Request) {
	view := adminUnavailableView{
		Example: "DATABASE_URL=postgres://user:password@localhost:5432/caltrain?sslmode=disable",
	}
	if DB == nil {
		view.Reason = "No database connection."
		view.Explanation = "The admin pages store and read their data in PostgreSQL, so they are " +
			"switched off when DATABASE_URL is unset or the connection could not be established at startup."
		view.Fix = "Set DATABASE_URL to a reachable database and restart. If it is already set, " +
			"check the startup logs for a connection error."
	} else {
		view.Reason = "The database connection has no credentials."
		view.Explanation = "The database is connected, but DATABASE_URL carries no username and " +
			"password. The admin pages use those same credentials for HTTP basic auth, so without " +
			"them there is nothing to authenticate against."
		view.Fix = "Include credentials in DATABASE_URL and restart. A URL such as " +
			"postgres://localhost:5432/caltrain connects successfully but leaves the admin section unreachable."
	}

	w.WriteHeader(http.StatusServiceUnavailable)
	renderAdminPage(w, "admin_unavailable", adminUnavailableHTML,
		newAdminPage("", "Admin unavailable", view))
}

// adminTab describes one entry in the admin tab bar.
type adminTab struct {
	ID    string
	Label string
	Href  string
}

// Tab identifiers, used to mark which tab is active on a page.
const (
	tabSupport       = "support"
	tabServiceAlerts = "servicealerts"
	tabDepartures    = "departures"
)

// adminTabs is the shared navigation shown on every admin page, in display order.
var adminTabs = []adminTab{
	{ID: tabSupport, Label: "Support requests", Href: "/admin/support"},
	{ID: tabServiceAlerts, Label: "Service alerts", Href: "/admin/servicealerts"},
	{ID: tabDepartures, Label: "Train departures", Href: "/admin/departures"},
}

// adminPage wraps page-specific data with the chrome the shared layout needs.
// Content templates reach their own data through .Data.
type adminPage struct {
	Title     string
	ActiveTab string
	Tabs      []adminTab
	Version   string
	Data      any
}

// newAdminPage builds a page model for the given tab and title.
func newAdminPage(activeTab, title string, data any) adminPage {
	return adminPage{
		Title:     title,
		ActiveTab: activeTab,
		Tabs:      adminTabs,
		Version:   BuildVersion(),
		Data:      data,
	}
}

// renderAdminPage renders a content template inside the shared admin layout.
// The content template must define a "content" block.
//
// Parsing failures are reported before any body is written, so a broken
// template cannot leave the client with a half-rendered page.
func renderAdminPage(w http.ResponseWriter, name, contentHTML string, page adminPage) {
	tmpl, err := template.New(name).Parse(adminLayoutHTML)
	if err == nil {
		_, err = tmpl.Parse(contentHTML)
	}
	if err != nil {
		log.Printf("admin template %q failed to parse: %v", name, err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", page); err != nil {
		log.Printf("admin template %q failed to render: %v", name, err)
	}
}
