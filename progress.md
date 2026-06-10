# Progress — All Improvements Implementation

## Branch: `feat/all-improvements-2026-06-09`
## Status: In Progress (26/41 items complete)

## Completed Items

### Wave 1 — Quick Wins ✅
| Item | Description | Commit |
|------|-------------|--------|
| 2 | Keyboard shortcuts dark mode | `92ba603` |
| 3 | Folder tree collapse button | `92ba603` |
| 28 | Sidebar icon swap (Members/Groups) | `92ba603` |
| 1 | Invite link fix (URL + /join route) | `fa21fce` |
| 25 | Collapse/show all toggle | `62f2c03` |
| 36 | New cell scroll into view | `62f2c03` |
| 6 | ConfirmDialog component | `b2cebaa` |
| 18 | Agent max_turns configurable (default 90) | `4d5be1a` |

### Wave 2 — Dashboard & Permissions ✅
| Item | Description | Commit |
|------|-------------|--------|
| 4 | Dashboard edit mode toggle (Edit/View links) | `332cef7` |
| 7 | Remove predefined permission presets | `332cef7` |
| 8 | Edit already placed widgets (pencil button) | `332cef7` |
| 10 | Per-widget play button with loading state | `332cef7` |

### Wave 3 — Backend Agent Features ✅
| Item | Description | Commit |
|------|-------------|--------|
| 17 | Skill trigger via /skill:name | `7d012d6`* |
| 16 | Skills discovery (list_skills tool) | `7d012d6`* |
| 30 | Subagent spawning fix | `fd436ec` |
| 31 | get_notebook_context tool | `7d012d6` |

### Wave 4 — Notebook Improvements ✅
| Item | Description | Commit |
|------|-------------|--------|
| 37 | Cell memoization (React.memo) | `48fca24` |
| 38 | Drag-and-drop reordering (@dnd-kit) | `48fca24` |
| 43 | Cell title markdown + remove description | `48fca24` |

### Other completed (from prior agents)
| Item | Description | Commit |
|------|-------------|--------|
| 22 | Personal access tokens | `30c7492` |
| 34 | MCP test connection button | `4c2f6a8` |
| 42 | OIDC provider test/validate button | `4c2f6a8` |
| 13 | Cell execution metrics | `832c11b` |

## Remaining Items (15)

| Item | Description | Wave |
|------|-------------|------|
| 19 | Full-screen image viewer with zoom | 5 |
| 39 | Markdown split preview mode | 5 |
| 40 | Bulk actions on file list | 5 |
| 29 | Audit filter improvements | 5 |
| 33 | Scalable skill/MCP selector | 5 |
| 27 | Global agent modal (Ctrl+K) | 5 |
| 9 | Audit cell execution logging | 6 |
| 11 | Dashboard permission system | 6 |
| 23 | Admin MOTD configuration | 6 |
| 32 | Import/export .ipynb | 6 |
| 21 | OpenAPI/Swagger docs | 7 |
| 24 | (duplicate of 6) | skip |
| 26 | (duplicate of 35) | skip |
| 20 | (duplicate of 19) | skip |
| 5 | "Run all" button | already done |
| 41 | (already done) | already done |

**True remaining: 12 unique items**

## Database Migrations Needed
- [ ] `cell_execution_logs` table (Item 13 — done in backend)
- [ ] `api_tokens` table (Item 22 — done)
- [ ] `motd_messages` table (Item 23)
- [ ] Remove `cells.description` column (Item 43 — frontend done, migration pending)
