# App Builder Guide

The canonical guide lives at [`internal/agent/guide/guide.md`](../internal/agent/guide/guide.md)
— the single source the `dashboard_guide` agent tool serves verbatim (embedded
via `internal/agent/guide/guide.go`). Edit that file when the app model
changes.

The web suite's cross-check (`web/src/app-builder/fieldHelp.test.ts`) asserts
every field in `web/src/app-builder/fieldHelp.ts` (the UI's inline hints and
HelpTip strings) appears in the guide, so the human surfaces and the agent
reference cannot drift apart.

Related docs: the wave-3 surfaces and decisions are summarized in
[`DESIGN.md`](../DESIGN.md) (Filter Scoping, Builder Guidance) and
[`FRONTEND.md`](../FRONTEND.md) (AppHealthPanel, HelpTip).
