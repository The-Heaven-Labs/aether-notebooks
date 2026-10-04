---
name: Aether Notebooks
description: Self-hosted collaborative SQL notebooks and dashboards — dark frame, light workspace, one quiet accent.
colors:
  accent: "#7c6faa"
  accent-strong: "#6a5e96"
  canvas: "#f5f5f5"
  panel: "#f9f9f9"
  surface: "#ffffff"
  cell-code: "#f7f7f7"
  ink: "#111111"
  ink-secondary: "#555555"
  ink-muted: "#6e6e6e"
  hairline: "#e8e8e8"
  hairline-light: "#efefef"
  chrome: "#1a1814"
  chrome-text: "#e8e4dc"
  chrome-muted: "#9e9690"
  chrome-border: "#2e2a24"
  success: "#2e7d32"
  danger: "#b85c5c"
  danger-strong: "#c0392b"
  warning: "#b89a4a"
  logo-navy: "#0b0f1a"
  chart-indigo: "#6366f1"
  chart-cyan: "#06b6d4"
  chart-emerald: "#10b981"
  chart-amber: "#f59e0b"
  chart-rose: "#f43f5e"
  chart-violet: "#8b5cf6"
  chart-sky: "#0ea5e9"
  chart-lime: "#84cc16"
typography:
  display:
    fontFamily: "DM Sans, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "40px"
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: "-0.5px"
  headline:
    fontFamily: "DM Sans, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "22px"
    fontWeight: 700
    lineHeight: 1.3
    letterSpacing: "-0.3px"
  title:
    fontFamily: "DM Sans, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.35
    letterSpacing: "-0.2px"
  body:
    fontFamily: "DM Sans, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "JetBrains Mono, Fira Code, ui-monospace, monospace"
    fontSize: "10px"
    fontWeight: 700
    lineHeight: 1.4
    letterSpacing: "0.08em"
  code:
    fontFamily: "JetBrains Mono, Fira Code, ui-monospace, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  xs: "3px"
  sm: "4px"
  md: "6px"
  lg: "8px"
  pill: "10px"
  circle: "50%"
spacing:
  xxs: "4px"
  xs: "8px"
  sm: "12px"
  md: "16px"
  lg: "24px"
  page: "32px"
components:
  button-primary:
    backgroundColor: "{colors.accent-strong}"
    textColor: "#ffffff"
    rounded: "{rounded.md}"
    padding: "8px 18px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.ink-secondary}"
    rounded: "{rounded.md}"
    padding: "8px 16px"
  button-icon:
    backgroundColor: "transparent"
    textColor: "{colors.ink-secondary}"
    rounded: "{rounded.sm}"
    padding: "3px 7px"
  field:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "8px 12px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "24px"
  chip:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-secondary}"
    rounded: "{rounded.sm}"
    padding: "1px 5px"
  nav-item:
    backgroundColor: "transparent"
    textColor: "{colors.chrome-muted}"
    rounded: "{rounded.md}"
    padding: "8px 12px"
  nav-item-active:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.accent}"
    rounded: "{rounded.md}"
    padding: "8px 12px"
---

# Design System: Aether Notebooks

## Overview

**Creative North Star: "The Observatory"**

Aether is a place from which people watch data move. The frame is a warm near-black — the 52px top bar and the left rail (#1a1814) — like the sky before the instruments come online. Inside it, the working surface glows like an illuminated desk: a near-white canvas (#f5f5f5), white cells, hairline rules, and data written in mono. The metaphor does real work: because the chrome is the darkest thing on screen, surfaces never need shadows to feel separated from it.

The mood is calm precision. One muted amethyst voice (#7c6faa) speaks for interactivity against an otherwise neutral system; data gets the only other chromatic outlets — status colors and the eight-series chart palette. Structure is drawn with 1px hairlines (#e8e8e8, #efefef) and small background tone shifts, not gradients or glass. State changes are brief and quiet (0.15s opacity and background shifts); nothing bounces, glows, or scales. Density is deliberate: data surfaces are tight (13px mono, 32px meta bars, 7px row padding), while the outer frame is generous (32px canvas padding), so the instrument reads as uncluttered even when a notebook does not.

The confirmed anti-reference is the generic gradient SaaS look: glassy panels, neon gradients, floating blobs, and decorative glow. Aether's restraint — flat surfaces, hairlines, one accent — is the identity, not a starting point to embellish. Dark mode does not invert the philosophy; it deepens the frame to #0a0a0a and lifts the accent to a pale amethyst (#9b8fc4) so the same calm survives on black.

**Key Characteristics:**
- Dark warm frame (top bar + rail) against a light canvas — the signature contrast; near-black on near-black in dark mode.
- A single Dusty Amethyst accent; everything non-interactive stays neutral.
- Flat surfaces at rest, 1px hairlines for structure, shadows only on floating layers.
- DM Sans for humans; JetBrains Mono for data, identifiers, counts, and micro-labels.
- Instrument-label typography: 10–11px uppercase tracked mono for badges and metadata.
- Electric indigo (#6366f1) confined to the logo mark and the chart series.
- Compact, monospace-led data surfaces inside a roomy 32px canvas.

## Colors

A neutral graphite-and-paper system carrying one dusty amethyst voice, a warm near-black chrome frame, and a separate data palette led by electric indigo.

### Primary
- **Dusty Amethyst** (#7c6faa): the interactive voice. Links, active navigation, primary buttons, focus outlines, drag placeholders. Muted enough to read as editorial rather than electric; bright lavender in dark mode (#9b8fc4).
- **Deep Amethyst** (#6a5e96): the pressed/hover deepening of the same voice, and the fill behind white text on primary buttons (`--button-primary-bg`). In dark mode the button fill inverts to light lavender (#b8a9d8) with near-black text (#111) so primary actions stay the brightest thing in the panel.

### Secondary
- **Electric Indigo** (#6366f1): identity and data only — the logo tile color and the first chart series. It must not be used for buttons, links, or focus states.
- **Logo Navy** (#0b0f1a): the mark's background; the deepest navy in the brand set, warmer and bluer than the app chrome.

### Tertiary — Data Series
The eight-series chart palette, in order: indigo (#6366f1), cyan (#06b6d4), emerald (#10b981), amber (#f59e0b), rose (#f43f5e), violet (#8b5cf6), sky (#0ea5e9), lime (#84cc16). Dark mode lightens every step (#818cf8, #22d3ee, #34d399, #fbbf24, #fb7185, #a78bfa, #38bdf8, #a3e635) to hold contrast on near-black cards. Series colors are assigned in order and must not be repurposed as UI accents.

### Semantic
- **Success** (#2e7d32), **Danger** (#b85c5c, with #c0392b for full-strength states like delete hovers), **Warning** (#b89a4a). Each has a tinted wash for backgrounds — success #f0f9f0, error #fdf5f5, warning #fffdf0 — with matching borders (#f5d0d0, #f5d78e) and darkened text (#9a2828, #8a6028). Status is usually expressed as colored text plus an icon, not as a filled badge.

### Neutral
- **Canvas** (#f5f5f5): the page the workspace sits on.
- **Panel** (#f9f9f9): toolbars, parameter bars, subtle recessed strips.
- **Surface** (#ffffff): cards, cells, modals, editors — the "paper".
- **Code Tint** (#f7f7f7): code cells and CodeMirror backgrounds, a half-step cooler than canvas.
- **Ink** (#111111) / **Ink Secondary** (#555555) / **Ink Muted** (#6e6e6e): primary, supporting, and metadata text. Muted is for timestamps, counts, placeholders — never for body copy.
- **Hairline** (#e8e8e8) / **Hairline Light** (#efefef): the two border weights that carry all structure. Hairline for card and control borders, hairline-light for internal dividers (meta bars, table rows).
- **Chrome** (#1a1814) / **Chrome Text** (#e8e4dc) / **Chrome Muted** (#9e9690) / **Chrome Border** (#2e2a24): the warm dark frame — top bar, side rail, login brand panel. In dark mode chrome deepens to #0a0a0a with #1a1a1a borders, keeping warmth in the text (#f0ece4).

Dark mode is a first-class theme, not a filter: the full mapping lives in `web/src/styles/theme.css`, where each token resolves to its dark counterpart (canvas #141414, surfaces #1c1c1c–#242424, hairlines #2e2e2e/#242424, ink #e8e8e8).

### Named Rules
**The One Voice Rule.** The Dusty Amethyst accent covers at most ~10% of any screen. Its rarity is what makes it read as "interactive"; when everything is purple, nothing is.

**The Dark Frame Rule.** The navigation chrome is always the darkest surface in view — #1a1814 in light mode, #0a0a0a in dark. The workspace is never darker than its frame.

**The Electric-Indigo Rule.** #6366f1 belongs to the mark and the chart series. It is never a button, link, or focus color; that is the amethyst family's job.

## Typography

**Display Font:** DM Sans (with -apple-system, BlinkMacSystemFont, sans-serif)
**Body Font:** DM Sans (same stack)
**Label/Mono Font:** JetBrains Mono (with Fira Code, ui-monospace, monospace)

**Character:** A humanist geometric sans that stays quiet at UI sizes, paired with a precise engineering mono. The division is semantic, not stylistic: DM Sans speaks for the product and its people, JetBrains Mono speaks for data and the machine. The serif in the logo philosophy lives on the wordmark only; the interface itself never sets serif type.

### Hierarchy
- **Display** (700, 40px, line-height 1.15, tracking -0.5px): the login brand title only — the one place type is allowed to be large.
- **Headline** (700, 22px, line-height 1.3, tracking -0.3px): page titles ("Files", "Connectors", notebook titles in headers).
- **Title** (700, 18px, line-height 1.35, tracking -0.2px): section headings, modal titles, empty-state titles; card titles drop to 15px/600–700.
- **Body** (400, 14px, line-height 1.5): interface copy and form text; secondary copy drops to 13px and metadata to 12px, both staying in DM Sans.
- **Code / Data** (400, 13px): JetBrains Mono for cell code, result tables, connectors, identifiers, and measurement footers (timings, row counts).
- **Label** (700, 10–11px, tracking 0.06–0.08em, uppercase): JetBrains Mono instrument markings — cell type badges ("SQL", "MD"), column type badges, row/column counts, sidebar section headers, keyboard hints.

### Named Rules
**The Two Scripts Rule.** If a human reads it as language, it is DM Sans. If it is code, data, an identifier, a count, or an instrument marking, it is JetBrains Mono. Never mix: no mono body copy, no sans code.

**The Instrument Label Rule.** Micro-labels are engraved, not spoken: 10–11px, uppercase, letter-spaced mono. They label instruments (badges, counts, types) and never form sentences.

## Layout

The app is a fixed viewport shell. A 52px top bar spans the top; a left rail sits beneath it at 48px collapsed or 200px expanded (width transitions 0.2s ease, state persisted per user), and the content canvas fills the rest with 32px padding. File-browser and settings surfaces use a two-panel layout: a 240px tree panel on the left (collapsible to a 7px edge handle) and a content area padded at 20px.

Notebooks stack cells vertically with 16px gaps. Each cell is a full-width card: a meta bar at min-height 32px (6px 16px padding) holding the run button, type badge, connector selector, and actions; an editor padded 14px 16px with a 72px minimum; then output. Result tables scroll inside a 340px-max-height box with a sticky header. Side panels (schema, parameters, history, schedules, agent chat) attach to the same shell as bordered rails rather than floating.

Forms and focused flows center at ~400px minimum width; the login page is a split screen — a 420px dark brand panel (chrome colors, display type, three feature bullets) beside the form panel. Empty states center a 56px icon tile (canvas-tinted, hairline border, 4px radius) above an 18px title and 14px description.

Breakpoints: 767px (top-bar hamburger, rail becomes a 280px drawer with a scrim) and 1024px (multi-column adaptations to stacked layouts, reduced padding). The rhythm scale is 4 / 8 / 12 / 16 / 24 / 32px — tight inside data surfaces (7–9px table rows, 6px meta bars), generous around them (32px canvas). Density is a feature of the content, not the frame.

## Elevation & Depth

The system is flat by default. Resting cards, cells, and panels carry no shadow (`--shadow-sm: none`); separation comes from 1px hairlines and background tone steps (canvas → surface → elevated). Depth belongs to two things: the dark chrome frame, which sits visually "behind" the workspace by being nearly black, and truly floating layers, which are the only elements allowed to cast shadows.

### Shadow Vocabulary
- **Floating mid** (`box-shadow: 0 2px 16px rgba(0,0,0,0.10)`; dark mode `0 2px 16px rgba(0,0,0,0.4)`): dropdown menus, modals, the profile menu — anything overlapping content.
- **Floating large** (`box-shadow: 0 8px 32px rgba(0,0,0,0.18)`; dark mode `0 8px 32px rgba(0,0,0,0.5)`): large overlays and drawers.
- **Scrim** (`--bg-overlay`: rgba(0,0,0,0.35) light / rgba(0,0,0,0.6) dark; drawers use rgba(0,0,0,0.3)): the dim layer under modals and slide-overs, separating the floating layer without blur.

### Named Rules
**The Flat-By-Default Rule.** Surfaces are flat at rest. A shadow appears only when an element genuinely floats above the page — menu, modal, drawer. If it is inline, it gets a hairline, never a shadow.

## Shapes

Form language is rectilinear and precise, with radii kept to a small, deliberate scale. Depth comes from edges, not curves.

- **3px** — micro elements: inline code chips, `kbd` hints, tiny pills.
- **4px** — the default for contained surfaces: cells, cards, modals, dropdown panels, badges, buttons inside the notebook. This is the system's default radius.
- **6px** — interactive controls: action buttons, text fields, sidebar nav items, list rows.
- **8px** — floating layers: profile menu, dropdown sheets, some dialog variants.
- **10px** — pill badges only (e.g. the output truncation badge).
- **50%** — avatars and status dots; nothing else is fully round.

Borders are always 1px: `var(--border)` for the outer edge of a surface, `var(--border-light)` for internal dividers. No 2px decorative borders, no dashed borders except the deliberate "add" affordance (parameter rows), no rounded-full buttons. Icons are Lucide stroke icons at 10–18px; no filled or duotone icon treatments.

### Named Rules
**The Three-Radius Rule.** Contained surfaces 4px, controls 6px, floating layers 8px. 3px is for micro elements and 10px for pills; anything else is drift, not a decision.

**The Hairline Rule.** Adjacent surfaces are separated by a 1px hairline in `--border` / `--border-light`. Never replace a hairline with a shadow, and never stack border + shadow on the same resting edge.

## Components

### Buttons
- **Shape:** 6px radius; the notebook's compact controls and icon buttons step down to 4px. Padding 8px 18px for primary, 8px 16px for secondary, 3px 7px for icon buttons.
- **Primary:** Deep Amethyst fill with white text (dark mode: light lavender fill with near-black text) — the `--button-primary-bg` / `--button-primary-text` pair exists precisely for this inversion. 13px/600 text.
- **Hover / Focus:** global hover is a 0.15s opacity drop to 0.9, not a color change; keyboard focus is a 2px amethyst outline with 2px offset, always visible. No lift, glow, or scale.
- **Secondary:** transparent fill, 1px hairline border, secondary ink text; icon buttons are the same at 4px radius and 12px glyphs. Delete actions gain red on hover only (`--error-full` text on `--error-light`).

### Chips / Badges
- **Type badges:** 10px uppercase JetBrains Mono, 700, tracked; canvas fill, hairline-light border, 4px radius, 1px 5px padding. They read as engraved plates ("SQL", "MD", column types).
- **Status badges:** colored text plus a small icon, 12px/600, no fill — status is stated, not stamped. Semantic washers (tinted background + border + dark text) are reserved for banners and inline messages.
- **Pills:** the output truncation badge is the exception that proves the scale: 10px radius, amethyst-light fill, 10px text.

### Cards / Containers
- **Corner style:** 4px radius.
- **Background:** surface white on the canvas; code cells sit a half-tone cooler (#f7f7f7 editor area) inside the same white card.
- **Shadow strategy:** none at rest (see Elevation) — a single hairline border does the work.
- **Border:** 1px `--border`, with `--border-light` dividers between meta bar, editor, and output.
- **Internal padding:** 24px for standalone form cards; cell internals use 6px 16px (meta bar) and 14px 16px (editor); list rows 12–16px. `overflow: hidden` clips content to the radius.

### Inputs / Fields
- **Style:** 1px hairline border, 6px radius for form fields (4px for inline notebook controls), canvas or surface background, 13–14px DM Sans text (mono for parameter names, connector selectors, and code-adjacent inputs).
- **Focus:** the global 2px amethyst outline at 2px offset — no glow, no border-color swap, no custom ring.
- **Placeholder:** secondary ink (#555), never muted for field hints. Error text uses `--error` at 12–13px; disabled controls keep native dimming without custom styling.

### Navigation
- The dark rail is the system's most distinctive surface: warm near-black (#1a1814; #0a0a0a dark) with 13px/500 items, 6px radius, 8px 12px padding, chrome-muted text (#9e9690).
- **Hover:** white at 5% opacity, no text change beyond brightening.
- **Active:** canvas-tinted background with amethyst text (dark mode: #242430 with #9b8fc4) — the active item is marked, not filled.
- **Structure:** a hairline separates the rail's "AI Agents" section label — an Instrument Label (uppercase tracked mono) — from the main nav. The top bar matches the rail: 52px, logo grid + wordmark left, org name and avatar right; the avatar is a 30px circle in amethyst-light with an amethyst hairline.

### The Cell (Signature)
The notebook cell is the component the product is built around. It is a white 4px-radius card with a hairline border and no shadow, read top to bottom as a mini workbench: meta bar → title → editor → output → measurement footer. The run control is the only saturated element in the cell (amethyst fill, white text, 12px/600); everything else is neutral and icon-sized. Outputs switch between a table and a chart through a 2px-inset segmented toggle (hairline-light trough, 4px radius, active button on white card with its own hairline). Tables set 13px mono on a sticky white header, 7px vertical row padding, hairline-light row rules, and italic muted nulls; captions ("X rows · Y columns") are Instrument Labels. Truncation, errors, and timing all speak in the same measured voice: error cards use the semantic red washer with an uppercase "ERROR" label, and the footer reports connect/query/render milliseconds in muted 11px mono.

### Empty States
A centered 56px icon tile (amethyst-light fill, hairline border, 4px radius) over an 18px/700 title, a 14px secondary description, and at most one primary button. No illustration, no gradient — an unlit instrument waiting for data.

## Do's and Don'ts

### Do:
- **Do** keep the frame dark and the workspace light: chrome stays on #1a1814 (light) / #0a0a0a (dark) while content surfaces stay #ffffff / #f5f5f5 (dark: #1c1c1c–#141414).
- **Do** separate surfaces with 1px `--border` / `--border-light` hairlines and background tone shifts; shadows are for menus, modals, and drawers only.
- **Do** use the Dusty Amethyst family for every interactive element — links, active nav, primary buttons, focus rings, drag placeholders — and keep it to roughly ≤10% of a screen.
- **Do** set data and identifiers in JetBrains Mono (13px) and instrument markings as 10–11px uppercase tracked labels; keep everything human in DM Sans.
- **Do** stay on the radius scale: 4px contained surfaces, 6px controls, 8px floating layers, 3px micro, 10px pills, 50% avatars.
- **Do** keep state changes to 0.15s ease (0.2s for layout) and express hover as an opacity or background shift.
- **Do** keep focus visible everywhere: 2px amethyst outline, 2px offset, on every interactive element.
- **Do** use the chart series palette in order for data, and semantic washes (tint + border + dark text) for status banners.

### Don't:
- **Don't** introduce gradient panels, glassmorphism, neon glow, or decorative blobs — confirmed anti-reference; flat + hairline is the identity.
- **Don't** use electric indigo #6366f1 for buttons, links, or focus states; it belongs to the mark and the chart series.
- **Don't** add a new radius per component: no rounded-full buttons, no 10px cards "because they look softer". The observed 10px in panels like `WarehouseTableGrants` is drift, not precedent.
- **Don't** give resting cards or cells a shadow, and don't replace a structural hairline with one.
- **Don't** animate state changes longer than 0.2s or use scale/bounce/spring effects on hover.
- **Don't** use off-palette colors for state feedback (the #3b82f6 blue in the cell-flash keyframe is legacy drift — use the amethyst family).
- **Don't** mix scripts: no mono prose, no sans code, no serif in the interface.
- **Don't** fill status badges with color; state status with colored text + icon, and reserve washes for message surfaces.
