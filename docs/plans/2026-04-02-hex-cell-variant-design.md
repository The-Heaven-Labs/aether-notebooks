# Cell Variant A — Left Rail + Hover Chrome

**Date:** 2026-04-02  
**Inspired by:** hex.tech notebook design patterns

## Design

A Storybook-only prototype (`CellVariantHex`) that explores a Hex-inspired cell layout adapted to hnb's existing purple/warm-beige design system.

### Key changes from current cell

| Current | Variant A |
|---|---|
| Toolbar always visible at top | Toolbar fades in on hover, absolutely positioned top-right |
| Run button in toolbar bar | Run button circle on left rail, appears on hover |
| Heavy top-bar chrome | 4px colored left border as the only constant chrome |
| Border always visible | Border appears on hover; idle cells are borderless |
| Connector as select dropdown | Connector shown as a pill badge in header |

### Structure

```
┌──────────────────────────────────────────────────────────────────┐
│▌  [◉ Postgres] Revenue by Month          [↑][↓][👁][⌨][⏱][✕]  │  ← header (hover toolbar fades in)
│▌  SELECT account_id,                                             │
│▌    SUM(amount) AS revenue               ← code editor area     │
│▌  FROM orders                                                    │
│▌  GROUP BY 1                                                     │
├──────────────────────────────────────────────────────────────────┤
│▌  ┌─────────┬──────────┐                 ← output table         │
│▌  │account  │ revenue  │                                         │
│▌  ├─────────┼──────────┤                                         │
│▌  │ A-001   │ 12,400   │                                         │
│▌  └─────────┴──────────┘                                         │
├──────────────────────────────────────────────────────────────────┤
│▌  Saved 2m ago                           Last run: 14:32:01     │  ← status bar
└──────────────────────────────────────────────────────────────────┘

▌ = 4px left rail (purple for SQL, slate-blue for markdown)
▶ = play button circle extends from rail on hover
```

### Design tokens (all from existing theme.css)

- Rail SQL: `var(--accent)` (#7c6faa)
- Rail MD: `#6b9dd8`
- Cell idle: no border, `var(--shadow-sm)`
- Cell hover: `border: 1px solid var(--border)`, `var(--shadow-md)`
- Toolbar: absolute `top: 8px right: 10px`, opacity 0→1 on hover

## Storybook Stories

- `Default` — SQL cell, idle
- `Running` — SQL cell, spinner in run button
- `WithOutput` — SQL cell with result table
- `SourceHidden` — header + output, no code
- `MarkdownCell` — blue-rail text cell
- `Collapsed` — collapsed pill state
