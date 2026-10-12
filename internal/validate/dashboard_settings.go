package validate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/the-heaven-labs/aether/internal/models"
)

// Dashboard app-surface bounds. They live here (not in internal/api) because
// both the REST handlers and the agent dashboard tools validate the same
// settings, and internal/agent cannot import internal/api.
const (
	maxAppTitleRunes    = 120
	maxAppSubtitleRunes = 200

	maxAppThemePaletteColors = 12
	maxAppThemeRadius        = 24

	maxDashboardTabs          = 10
	maxDashboardTabIDLen      = 64
	maxDashboardTabTitleRunes = 60
)

// dashboardTabIDPattern is the charset a tab id (and a widget's layout.tab)
// must match: lowercase alphanumerics and dashes, starting alphanumeric.
var dashboardTabIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// DashboardAppSettings bounds the app surface configuration. Unknown JSON keys
// are already dropped by the struct round-trip; this only rejects values that
// would render incorrectly.
func DashboardAppSettings(app *models.DashboardAppSettings) error {
	if app == nil {
		return nil
	}
	if len([]rune(app.Title)) > maxAppTitleRunes {
		return fmt.Errorf("app title must be at most %d characters", maxAppTitleRunes)
	}
	if len([]rune(app.Subtitle)) > maxAppSubtitleRunes {
		return fmt.Errorf("app subtitle must be at most %d characters", maxAppSubtitleRunes)
	}
	if app.Width != "" && app.Width != "fixed" && app.Width != "full" {
		return fmt.Errorf(`app width must be "fixed" or "full"`)
	}
	if app.Layout != "" && app.Layout != "grid" && app.Layout != "custom" {
		return fmt.Errorf(`app layout must be "grid" or "custom"`)
	}
	return dashboardAppTheme(app.Theme)
}

// dashboardAppTheme bounds the app surface's token overrides.
func dashboardAppTheme(theme *models.DashboardAppTheme) error {
	if theme == nil {
		return nil
	}
	colors := []struct {
		name  string
		value string
	}{
		{"accent", theme.Accent},
		{"background", theme.Background},
		{"surface", theme.Surface},
		{"text", theme.Text},
		{"border", theme.Border},
	}
	for _, c := range colors {
		if c.value != "" && !IsHexColor(c.value) {
			return fmt.Errorf("app theme %s must be a hex color like #rrggbb", c.name)
		}
	}
	if theme.Radius != nil && (*theme.Radius < 0 || *theme.Radius > maxAppThemeRadius) {
		return fmt.Errorf("app theme radius must be between 0 and %d", maxAppThemeRadius)
	}
	switch theme.FontFamily {
	case "", "dm-sans", "jetbrains-mono", "system":
	default:
		return fmt.Errorf(`app theme font_family must be "dm-sans", "jetbrains-mono", or "system"`)
	}
	switch theme.ColorScheme {
	case "", "auto", "light", "dark":
	default:
		return fmt.Errorf(`app theme color_scheme must be "auto", "light", or "dark"`)
	}
	if len(theme.ChartPalette) > maxAppThemePaletteColors {
		return fmt.Errorf("app theme chart_palette must have at most %d colors", maxAppThemePaletteColors)
	}
	for _, c := range theme.ChartPalette {
		if !IsHexColor(c) {
			return fmt.Errorf("app theme chart_palette entries must be hex colors like #rrggbb")
		}
	}
	return nil
}

// IsHexColor reports whether s is a #rgb or #rrggbb hex color.
func IsHexColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// DashboardTabs bounds the dashboard's tab list: at most 10 tabs with unique
// lowercase ids and a non-empty, bounded title.
func DashboardTabs(tabs []models.DashboardTab) error {
	if len(tabs) > maxDashboardTabs {
		return fmt.Errorf("at most %d tabs are allowed", maxDashboardTabs)
	}
	seen := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		if tab.ID == "" {
			return fmt.Errorf("tab id is required")
		}
		if err := WidgetTab(tab.ID); err != nil {
			return err
		}
		if seen[tab.ID] {
			return fmt.Errorf("duplicate tab id %q", tab.ID)
		}
		seen[tab.ID] = true
		if strings.TrimSpace(tab.Title) == "" {
			return fmt.Errorf("tab title is required")
		}
		if len([]rune(tab.Title)) > maxDashboardTabTitleRunes {
			return fmt.Errorf("tab title must be at most %d characters", maxDashboardTabTitleRunes)
		}
	}
	return nil
}

// WidgetTab bounds a widget's layout.tab. An empty tab is allowed (the widget
// falls back to the first tab); otherwise it must match the tab id charset and
// length.
func WidgetTab(tab string) error {
	if tab == "" {
		return nil
	}
	if len(tab) > maxDashboardTabIDLen || !dashboardTabIDPattern.MatchString(tab) {
		return fmt.Errorf("invalid tab id %q (lowercase letters, digits, and dashes only, must start with a letter or digit, at most %d characters)", tab, maxDashboardTabIDLen)
	}
	return nil
}

// DashboardSettingsValue validates the `app` and `tabs` blocks of a raw
// settings map. The update paths decode settings as a map (shallow merge
// through the dashboard document), so the values arrive untyped; decode them
// into the models to reuse the bounds and enum checks above.
func DashboardSettingsValue(settings map[string]any) error {
	if settings == nil {
		return nil
	}
	if err := dashboardAppSettingsValue(settings["app"]); err != nil {
		return err
	}
	raw, ok := settings["tabs"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("invalid tabs")
	}
	var tabs []models.DashboardTab
	if err := json.Unmarshal(b, &tabs); err != nil {
		return fmt.Errorf("invalid tabs")
	}
	return DashboardTabs(tabs)
}

// dashboardAppSettingsValue validates the `app` block of a raw settings map.
// The update path decodes settings as a map (shallow merge through the
// dashboard document), so the app value arrives untyped; decode it into the
// model to reuse the bounds and enum checks above.
func dashboardAppSettingsValue(raw any) error {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("invalid app settings")
	}
	var app models.DashboardAppSettings
	if err := json.Unmarshal(b, &app); err != nil {
		return fmt.Errorf("invalid app settings")
	}
	return DashboardAppSettings(&app)
}
