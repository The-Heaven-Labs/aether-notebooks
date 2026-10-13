// Package dashboardreview implements the dashboard cohesion heuristics shared
// by the `review_dashboard` agent tool and the editor's App health panel: a
// pure pass over a dashboard document projection that returns prioritized,
// actionable findings, each naming the surface that fixes it.
//
// The rules are deterministic and read-only. Layout mirrors the app's per-
// chapter grids (an empty or unknown widget tab falls back to the first
// chapter, exactly like the renderer); content checks the stored widget
// config shapes; surface checks the app block, theme guardrails, and the
// publish state.
package dashboardreview

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/models"
)

// Severity orders the report: errors first, then warnings, then info.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is one actionable review result. ChapterID/WidgetID are empty when
// the finding is not scoped to one; FixTool names the agent tool call (or
// editor surface) that resolves it.
type Finding struct {
	Severity  Severity `json:"severity"`
	Category  string   `json:"category"` // layout | content | surface
	ChapterID string   `json:"chapter_id,omitempty"`
	WidgetID  string   `json:"widget_id,omitempty"`
	Message   string   `json:"message"`
	FixTool   string   `json:"fix_tool,omitempty"`
}

// Options carries the publish-state inputs the projection itself cannot know.
type Options struct {
	// PublishedHash / DraftHash are the latest version's content hash and the
	// draft's hash (dashboarddoc.ContentHash). Differing non-empty hashes mean
	// unpublished changes.
	PublishedHash string
	DraftHash     string
}

// Report is the review result shared by the agent tool and the REST endpoint:
// the findings plus the app links. PublicURL is empty (and omitted from JSON)
// when the dashboard is not shared; ViewURL/PreviewURL fall back to
// app-relative paths when the caller has no base URL.
type Report struct {
	DashboardID string    `json:"dashboard_id"`
	Findings    []Finding `json:"findings"`
	Count       int       `json:"count"`
	ViewURL     string    `json:"view_url"`
	PreviewURL  string    `json:"preview_url"`
	PublicURL   string    `json:"public_url,omitempty"`
}

// BuildReport assembles a report from a projection: the cohesion findings plus
// the viewer, draft-preview, and (when shared) public links. The caller owns
// loading the projection, the published hash, and the public token; this keeps
// the agent tool and the REST endpoint byte-identical.
func BuildReport(dashboardID string, proj *dashboarddoc.Projection, opts Options, baseURL, publicToken string) Report {
	findings := Review(proj, opts)
	if findings == nil {
		findings = []Finding{}
	}
	base := strings.TrimRight(baseURL, "/")
	report := Report{
		DashboardID: dashboardID,
		Findings:    findings,
		Count:       len(findings),
		ViewURL:     base + "/dashboards/" + dashboardID + "/view",
		PreviewURL:  base + "/dashboards/" + dashboardID + "/view?preview=draft",
	}
	if publicToken != "" {
		report.PublicURL = base + "/public/dashboards/" + publicToken
	}
	return report
}

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}`)
var headingPattern = regexp.MustCompile(`(?m)^#{1,6}\s`)

// WCAG 2.1 AA minimums, mirroring web/src/utils/appTheme.ts.
const (
	textContrastMin   = 4.5
	accentContrastMin = 3
)

// Review runs every rule and returns the findings ordered by severity, then
// chapter, then widget.
func Review(proj *dashboarddoc.Projection, opts Options) []Finding {
	if proj == nil {
		return nil
	}
	tabs := tabsOf(proj)
	gridCols := gridColsOf(proj)

	var findings []Finding
	findings = append(findings, layoutFindings(proj, tabs, gridCols)...)
	findings = append(findings, contentFindings(proj, tabs)...)
	findings = append(findings, surfaceFindings(proj, opts)...)

	sortFindings(findings)
	return findings
}

func sortFindings(findings []Finding) {
	rank := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		if a.ChapterID != b.ChapterID {
			return a.ChapterID < b.ChapterID
		}
		if a.WidgetID != b.WidgetID {
			return a.WidgetID < b.WidgetID
		}
		return a.Message < b.Message
	})
}

// layoutFindings checks the per-chapter grids: invalid bounds, overlaps,
// leading gaps, empty chapters, chapter balance, and widget tab placement.
func layoutFindings(proj *dashboarddoc.Projection, tabs []models.DashboardTab, gridCols int) []Finding {
	var findings []Finding
	ids := sortedWidgetIDs(proj)

	for _, id := range ids {
		w := proj.Widgets[id]
		chapter := effectiveTab(w.Layout.Tab, tabs)
		if w.Layout.Row < 0 || w.Layout.Col < 0 || w.Layout.Width <= 0 || w.Layout.Height <= 0 {
			findings = append(findings, Finding{
				Severity: SeverityError, Category: "layout", ChapterID: chapter, WidgetID: id,
				Message: fmt.Sprintf("widget %s has invalid dimensions (row/col must be ≥ 0 and width/height > 0)", id),
				FixTool: "set_dashboard_layout",
			})
			continue
		}
		if w.Layout.Col+w.Layout.Width > gridCols {
			findings = append(findings, Finding{
				Severity: SeverityError, Category: "layout", ChapterID: chapter, WidgetID: id,
				Message: fmt.Sprintf("widget %s exceeds the %d-column grid (col %d + width %d)",
					id, gridCols, w.Layout.Col, w.Layout.Width),
				FixTool: "set_dashboard_layout",
			})
		}
	}

	// Overlaps within the same effective chapter (identical coordinates in
	// different chapters coexist: each chapter is its own grid).
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := proj.Widgets[ids[i]], proj.Widgets[ids[j]]
			chapterA := effectiveTab(a.Layout.Tab, tabs)
			if chapterA != effectiveTab(b.Layout.Tab, tabs) {
				continue
			}
			if a.Layout.Col < b.Layout.Col+b.Layout.Width &&
				a.Layout.Col+a.Layout.Width > b.Layout.Col &&
				a.Layout.Row < b.Layout.Row+b.Layout.Height &&
				a.Layout.Row+a.Layout.Height > b.Layout.Row {
				message := fmt.Sprintf("widgets %s and %s overlap", ids[i], ids[j])
				if chapterA != "" {
					message = fmt.Sprintf("widgets %s and %s overlap in chapter %q", ids[i], ids[j], chapterA)
				}
				findings = append(findings, Finding{
					Severity: SeverityError, Category: "layout", ChapterID: chapterA, WidgetID: ids[i],
					Message: message, FixTool: "set_dashboard_layout",
				})
			}
		}
	}

	// Per-chapter grouping for gaps, empty chapters, and balance.
	counts := map[string]int{}
	minRow := map[string]int{}
	first := ""
	if len(tabs) > 0 {
		first = tabs[0].ID
	}
	for _, id := range ids {
		chapter := effectiveTab(proj.Widgets[id].Layout.Tab, tabs)
		counts[chapter]++
		row := proj.Widgets[id].Layout.Row
		if current, ok := minRow[chapter]; !ok || row < current {
			minRow[chapter] = row
		}
	}

	if len(tabs) > 0 {
		for _, t := range tabs {
			count := counts[t.ID]
			if count == 0 {
				findings = append(findings, Finding{
					Severity: SeverityWarning, Category: "layout", ChapterID: t.ID,
					Message: fmt.Sprintf("chapter %q has no widgets", t.ID),
					FixTool: "set_dashboard_layout",
				})
				continue
			}
			if minRow[t.ID] > 0 {
				findings = append(findings, Finding{
					Severity: SeverityInfo, Category: "layout", ChapterID: t.ID,
					Message: fmt.Sprintf("chapter %q starts at row %d; the app renders it compacted from row 0", t.ID, minRow[t.ID]),
					FixTool: "set_dashboard_layout",
				})
			}
		}
	} else if len(ids) > 0 && minRow[""] > 0 {
		findings = append(findings, Finding{
			Severity: SeverityInfo, Category: "layout",
			Message: fmt.Sprintf("the dashboard's widgets start at row %d; the app renders them compacted from row 0", minRow[""]),
			FixTool: "set_dashboard_layout",
		})
	}

	// Balance: a chapter with more than 3× the median widget count is a
	// composition smell (the median is the upper median; a zero median is
	// covered by the empty-chapter rule instead).
	if len(tabs) >= 2 {
		sortedCounts := make([]int, 0, len(tabs))
		for _, t := range tabs {
			sortedCounts = append(sortedCounts, counts[t.ID])
		}
		sort.Ints(sortedCounts)
		median := sortedCounts[len(sortedCounts)/2]
		if median >= 1 {
			for _, t := range tabs {
				if counts[t.ID] > 3*median {
					findings = append(findings, Finding{
						Severity: SeverityInfo, Category: "layout", ChapterID: t.ID,
						Message: fmt.Sprintf("chapter %q has %d widgets — more than 3× the median (%d)",
							t.ID, counts[t.ID], median),
						FixTool: "set_dashboard_layout",
					})
				}
			}
		}
	}

	// Tab placement: with chapters defined, an untabbed widget silently lands
	// in the first chapter and a stale tab name silently falls back.
	if len(tabs) > 0 {
		for _, id := range ids {
			tab := proj.Widgets[id].Layout.Tab
			if tab == "" {
				findings = append(findings, Finding{
					Severity: SeverityInfo, Category: "layout", ChapterID: first, WidgetID: id,
					Message: fmt.Sprintf("widget %s has no chapter; it renders in %q", id, first),
					FixTool: "set_dashboard_layout",
				})
				continue
			}
			if !tabExists(tab, tabs) {
				findings = append(findings, Finding{
					Severity: SeverityWarning, Category: "layout", ChapterID: first, WidgetID: id,
					Message: fmt.Sprintf("widget %s names chapter %q, which no longer exists; it falls back to %q",
						id, tab, first),
					FixTool: "set_dashboard_layout",
				})
			}
		}
	}

	return findings
}

// contentFindings checks the stored widget configs: metric completeness, table
// column formats, and text-panel substance.
func contentFindings(proj *dashboarddoc.Projection, tabs []models.DashboardTab) []Finding {
	var findings []Finding
	for _, id := range sortedWidgetIDs(proj) {
		w := proj.Widgets[id]
		chapter := effectiveTab(w.Layout.Tab, tabs)
		switch w.Type {
		case "metric":
			metric := configBlock(w.Config, "metric")
			if strValue(metric, "value_column") == "" {
				findings = append(findings, Finding{
					Severity: SeverityWarning, Category: "content", ChapterID: chapter, WidgetID: id,
					Message: fmt.Sprintf("metric %s has no value column", id),
					FixTool: "set_widget_config",
				})
			}
			if strValue(metric, "label") == "" {
				findings = append(findings, Finding{
					Severity: SeverityInfo, Category: "content", ChapterID: chapter, WidgetID: id,
					Message: fmt.Sprintf("metric %s has no label — viewers see the column name", id),
					FixTool: "set_widget_config",
				})
			}
		case "table":
			table := configBlock(w.Config, "table")
			columns, _ := table["columns"].([]any)
			if len(columns) == 0 {
				findings = append(findings, Finding{
					Severity: SeverityInfo, Category: "content", ChapterID: chapter, WidgetID: id,
					Message: fmt.Sprintf("table %s has no column formats — values render as stored", id),
					FixTool: "set_widget_config",
				})
			}
		case "text":
			markdown := strValue(configBlock(w.Config, "text"), "markdown")
			if strings.TrimSpace(markdown) == "" {
				findings = append(findings, Finding{
					Severity: SeverityWarning, Category: "content", ChapterID: chapter, WidgetID: id,
					Message: fmt.Sprintf("text panel %s is empty", id),
					FixTool: "set_widget_config",
				})
				continue
			}
			if !headingPattern.MatchString(markdown) {
				findings = append(findings, Finding{
					Severity: SeverityInfo, Category: "content", ChapterID: chapter, WidgetID: id,
					Message: fmt.Sprintf("text panel %s has no heading — start with a markdown heading (## …)", id),
					FixTool: "set_widget_config",
				})
			}
		}
	}
	return findings
}

// surfaceFindings checks the app block, theme guardrails, filter scoping, the
// publish state, and the projection's own warnings.
func surfaceFindings(proj *dashboarddoc.Projection, opts Options) []Finding {
	var findings []Finding
	app := configBlock(proj.Settings, "app")

	if strValue(app, "title") == "" {
		message := "app has no title"
		if proj.Title != "" {
			message = fmt.Sprintf("app has no title; viewers see %q", proj.Title)
		}
		findings = append(findings, Finding{
			Severity: SeverityInfo, Category: "surface", Message: message,
			FixTool: "update_dashboard_settings",
		})
	}

	findings = append(findings, themeContrastFindings(configBlock(app, "theme"))...)

	// Chapter scoping hides filters that no widget (transitively) uses: they
	// can never appear in any chapter's strip.
	if strValue(app, "filter_scope") == "tab" {
		used := usedVariableNames(proj)
		for _, variable := range proj.Variables {
			name := strValue(variable, "name")
			if name == "" || used[name] {
				continue
			}
			findings = append(findings, Finding{
				Severity: SeverityInfo, Category: "surface",
				Message: fmt.Sprintf("filter %q is used by no widget and never shows under chapter scoping", name),
				FixTool: "update_dashboard_settings",
			})
		}
	}

	if opts.PublishedHash != "" && opts.DraftHash != "" && opts.PublishedHash != opts.DraftHash {
		findings = append(findings, Finding{
			Severity: SeverityInfo, Category: "surface",
			Message: "the draft has unpublished changes — publish to update viewers",
			FixTool: "publish_dashboard",
		})
	}

	// Projection warnings are the document-level defects that refuse publish.
	for _, warning := range proj.Warnings {
		findings = append(findings, Finding{
			Severity: SeverityError, Category: "surface",
			Message: warning,
		})
	}

	return findings
}

// themeContrastFindings ports resolveAppTheme's guardrails for explicitly
// authored pairs (the scheme defaults are unknowable server-side, so only
// pairs the author set are evaluated).
func themeContrastFindings(theme map[string]any) []Finding {
	var findings []Finding
	text := strValue(theme, "text")
	background := strValue(theme, "background")
	surface := strValue(theme, "surface")
	accent := strValue(theme, "accent")

	check := func(label, fg, bg, bgName string, min float64) {
		ratio, ok := contrastRatio(fg, bg)
		if !ok || ratio >= min {
			return
		}
		findings = append(findings, Finding{
			Severity: SeverityWarning, Category: "surface",
			Message: fmt.Sprintf("theme %s %s fails contrast on %s %s (%.1f:1, needs %.1f:1)",
				label, fg, bgName, bg, ratio, min),
			FixTool: "update_dashboard_settings",
		})
	}
	if text != "" && background != "" {
		check("text", text, background, "background", textContrastMin)
	}
	if text != "" && surface != "" {
		check("text", text, surface, "surface", textContrastMin)
	}
	if accent != "" && background != "" {
		check("accent", accent, background, "background", accentContrastMin)
	}
	return findings
}

// usedVariableNames returns the variables reachable from any widget: direct
// SQL / markdown references plus the transitive closure over options queries
// and depends_on — the same closure the viewer's chapter scoping uses, so a
// variable that feeds a used filter is not reported as dead.
func usedVariableNames(proj *dashboarddoc.Projection) map[string]bool {
	type variableInfo struct {
		name       string
		tokens     []string
		optionsSQL string
		dependsOn  []string
	}
	byToken := map[string]*variableInfo{}
	byName := map[string]*variableInfo{}
	for _, variable := range proj.Variables {
		name := strValue(variable, "name")
		if name == "" {
			continue
		}
		info := &variableInfo{name: name}
		info.tokens = append(info.tokens, name)
		if strValue(variable, "type") == "date_range" {
			info.tokens = append(info.tokens, name+"_start", name+"_end")
		}
		if options := configBlock(variable, "options"); options != nil {
			if query := configBlock(options, "query"); query != nil {
				info.optionsSQL = strValue(query, "sql")
			}
		}
		if deps, ok := variable["depends_on"].([]any); ok {
			for _, d := range deps {
				if s, ok := d.(string); ok && s != "" {
					info.dependsOn = append(info.dependsOn, s)
				}
			}
		}
		byName[name] = info
		for _, token := range info.tokens {
			byToken[token] = info
		}
	}

	used := map[string]bool{}
	var queue []*variableInfo
	includeVariable := func(info *variableInfo) {
		if !used[info.name] {
			used[info.name] = true
			queue = append(queue, info)
		}
	}
	includeToken := func(token string) {
		if info, ok := byToken[token]; ok {
			includeVariable(info)
		}
	}

	for _, w := range proj.Widgets {
		if w.Query != nil {
			for _, token := range tokenPattern.FindAllStringSubmatch(*w.Query, -1) {
				includeToken(token[1])
			}
		}
		markdown := strValue(configBlock(w.Config, "text"), "markdown")
		for _, token := range tokenPattern.FindAllStringSubmatch(markdown, -1) {
			includeToken(token[1])
		}
	}

	for i := 0; i < len(queue); i++ {
		info := queue[i]
		for _, token := range tokenPattern.FindAllStringSubmatch(info.optionsSQL, -1) {
			includeToken(token[1])
		}
		for _, dep := range info.dependsOn {
			if target, ok := byName[dep]; ok {
				includeVariable(target)
			}
		}
	}
	return used
}

// --- shared helpers -------------------------------------------------------

func contains(s, substr string) bool { return strings.Contains(s, substr) }

func sortedWidgetIDs(proj *dashboarddoc.Projection) []string {
	ids := make([]string, 0, len(proj.Widgets))
	for id := range proj.Widgets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// configBlock returns a nested settings/config object as a map, tolerating
// the JSON-decoded shapes (map[string]any) and absent keys.
func configBlock(container map[string]any, key string) map[string]any {
	if container == nil {
		return nil
	}
	raw, ok := container[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case map[string]any:
		return v
	case map[string]string:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return out
	}
}

func strValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// tabsOf decodes settings["tabs"] into the model, accepting both the JSON-
// decoded shape and Go-constructed []models.DashboardTab values.
func tabsOf(proj *dashboarddoc.Projection) []models.DashboardTab {
	if proj == nil || proj.Settings == nil {
		return nil
	}
	raw, ok := proj.Settings["tabs"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var tabs []models.DashboardTab
	if err := json.Unmarshal(b, &tabs); err != nil {
		return nil
	}
	return tabs
}

func gridColsOf(proj *dashboarddoc.Projection) int {
	if proj == nil || proj.Settings == nil {
		return 12
	}
	n := 0
	switch v := proj.Settings["grid_cols"].(type) {
	case float64:
		n = int(v)
	case int:
		n = v
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			n = int(parsed)
		}
	}
	if n <= 0 {
		n = 12
	}
	return n
}

// effectiveTab mirrors the renderer (and validate.effectiveWidgetTab): a
// known layout.tab is itself; an empty or unknown tab falls back to the first
// chapter; "" when the dashboard defines no chapters.
func effectiveTab(tab string, tabs []models.DashboardTab) string {
	if len(tabs) == 0 {
		return ""
	}
	if tab != "" && tabExists(tab, tabs) {
		return tab
	}
	return tabs[0].ID
}

func tabExists(tab string, tabs []models.DashboardTab) bool {
	for _, t := range tabs {
		if t.ID == tab {
			return true
		}
	}
	return false
}

// --- WCAG contrast (Go port of web/src/utils/appTheme.ts) ------------------

func hexToRGB(hex string) (r, g, b int, ok bool) {
	hex = strings.TrimSpace(hex)
	if len(hex) != 4 && len(hex) != 7 {
		return 0, 0, 0, false
	}
	if hex[0] != '#' {
		return 0, 0, 0, false
	}
	digits := hex[1:]
	hexDigit := func(s string) int {
		switch {
		case s >= "0" && s <= "9":
			return int(s[0] - '0')
		case s >= "a" && s <= "f":
			return int(s[0]-'a') + 10
		case s >= "A" && s <= "F":
			return int(s[0]-'A') + 10
		default:
			return -1
		}
	}
	channel := func(i int) int {
		if len(digits) == 3 {
			d := digits[i : i+1]
			return hexDigit(d)*16 + hexDigit(d)
		}
		return hexDigit(digits[i*2:i*2+1])*16 + hexDigit(digits[i*2+1:i*2+2])
	}
	for _, c := range digits {
		if hexDigit(string(c)) < 0 {
			return 0, 0, 0, false
		}
	}
	return channel(0), channel(1), channel(2), true
}

func channelLuminance(value int) float64 {
	c := float64(value) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func relativeLuminance(r, g, b int) float64 {
	return 0.2126*channelLuminance(r) + 0.7152*channelLuminance(g) + 0.0722*channelLuminance(b)
}

// contrastRatio is the WCAG 2.1 ratio (1–21, order-independent); ok=false
// when either color cannot be parsed.
func contrastRatio(fg, bg string) (ratio float64, ok bool) {
	fgR, fgG, fgB, fgOK := hexToRGB(fg)
	bgR, bgG, bgB, bgOK := hexToRGB(bg)
	if !fgOK || !bgOK {
		return 0, false
	}
	fgLum := relativeLuminance(fgR, fgG, fgB)
	bgLum := relativeLuminance(bgR, bgG, bgB)
	maxLum, minLum := fgLum, bgLum
	if bgLum > fgLum {
		maxLum, minLum = bgLum, fgLum
	}
	return (maxLum + 0.05) / (minLum + 0.05), true
}
