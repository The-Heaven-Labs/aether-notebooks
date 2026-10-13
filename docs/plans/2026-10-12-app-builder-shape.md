# App Builder — Shape Decisions (builder guidance pass)

**Date:** 2026-10-12
**Status:** Confirmed with the author (shape round complete)
**Folds into:** `DESIGN.md` builder guidance patterns (Task 19)

The Impeccable shape pass for the app-builder guidance work (plan Task 17). The
visual world is already committed (`DESIGN.md`: dark frame / light workspace,
Dusty Amethyst, hairlines, mono instrument labels, "unlit instrument" empty
states); this pass fixes **patterns and copy** only — no new visual direction.

## Brief

- **Job / audience:** Authors arrive at the builder from a notebook or a blank
  slate; their job is to compose chapters, filters, and widgets into a
  shareable app, then publish. Success = an author who has never built an app
  can go empty → published without docs — and the agent can do the same
  through tools.
- **Direction:** Guidance lives inside existing chrome — one-line hints in
  muted 11–12px, HelpTips as small popovers, no new panels or decoration.
  Copy is specific to Aether's model (chapters, filters, draft → publish,
  query widgets), never generic dashboard-builder filler.
- **Scope (deepest treatment):** the draft-to-publish path — editor empty
  state, add-widget picker, config drawer (metric/text/table/appearance/chart),
  App settings (tabs / filters / theme), versions panel. App-view empty states
  and org branding get lighter one-liners.
- **States:** empty/first-run is the teaching moment; errors stay actionable;
  hints never obstruct expert use (progressive disclosure).

## Decisions

| Decision | Choice |
|---|---|
| Voice | **Quiet instrument, second person.** Terse factual lines; mono for field/token names; one idea per line; no exclamation, no "just/simply". |
| Help affordance | **Progressive.** One-line inline hints orient a section; a keyboard/touch `HelpTip` trigger only where a value needs an example (hex colors, formats, comparison/sparkline columns). `aria-describedby`-wired, no hover-only behavior. |
| Pass focus | **Draft-to-publish path** gets the deep treatment; viewer empty states + branding stay light. |

### Voice examples (the register to hold)

- Picker: "Metric — one number from a query." / "Text — markdown with live
  `{{filters}}`."
- Filter scope: "Each chapter shows the filters its widgets use, plus the
  filters those depend on."
- Versions: "Publish the draft to create v1." (existing line stays).
- Drawer field: "Columns without a format render as stored."

## Anti-goals

- No new panels, badges, illustrations, or chrome for guidance.
- No restyling of existing controls; no layout churn from hints.
- Do not rewrite copy that already works (e.g. the versions empty-state line).
- Never generic ("Configure your widget here") — name the model.

## Application map (for Tasks 15–16)

| Surface | Treatment |
|---|---|
| Editor empty state (new dashboard) | Full teaching: chapters (lede → readings → evidence), picker next step. |
| Add-widget picker | One-line descriptor + micro-example per type; source explained in one line. |
| Config drawer | Section one-liners; HelpTip on color/format/comparison/sparkline fields. |
| App settings panel | App fields, chapters, filter scope (inference explained), theme guardrails. |
| Versions panel | Draft vs published in one line; empty state kept as-is. |
| App view / public empty states | One-liner: what viewers will see. |
| Org branding | One-liner per field: what it affects across apps. |
| Health panel (new) | Quiet checklist: severity, message, chapter, jump-to-widget + fix hint. |
