package caltraingateway

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed web/admin_layout.html
var adminLayoutHTML string

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
