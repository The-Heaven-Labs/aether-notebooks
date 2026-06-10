# Implementation Tracker

**Branch:** `feat/all-improvements-2026-06-09`
**Started:** 2026-06-09
**Total items:** 43 (minus Item 5 already done, Item 41 already done = 41 to implement)

## Wave 1 - Quick Wins & Isolated Fixes ✅ COMPLETE
| Item | Description | Status |
|------|-------------|--------|
| 2 | Keyboard shortcuts dark mode | ✅ |
| 3 | Folder tree collapse button | ✅ |
| 28 | Sidebar icon swap | ✅ |
| 1 | Invite link fix | ✅ |
| 25 | Collapse/show all | ✅ |
| 36 | New cell scroll into view | ✅ |
| 6 | ConfirmDialog component | ✅ |
| 18 | Agent max_turns configurable | ✅ |

## Wave 2 - Backend & Dashboard (PENDING)
| Item | Description | Status |
|------|-------------|--------|
| 18 | Agent max_turns configurable | ⬜ |
| 17 | Skill trigger /skill:name | ⬜ |
| 16 | Skills discovery (list_skills) | ⬜ |
| 4 | Dashboard edit mode unification | ⬜ |
| 8 | Edit placed widgets | ⬜ |
| 10 | Widget play button + loading | ⬜ |
| 7 | Remove permission presets | ⬜ |

## Wave 3 - Notebook Improvements (PENDING)
| Item | Description | Status |
|------|-------------|--------|
| 37 | Cell memoization | ⬜ |
| 25 | Collapse/show all | ⬜ |
| 39 | Markdown split preview | ⬜ |
| 43 | Cell title markdown + remove description | ⬜ |
| 38 | Drag-and-drop reordering | ⬜ |

## Wave 4 - Backend Features (PENDING)
| Item | Description | Status |
|------|-------------|--------|
| 13 | Cell execution metrics | ⬜ |
| 30 | Subagent spawning fix | ⬜ |
| 31 | Notebook context tool | ⬜ |
| 22 | Personal access tokens | ⬜ |
| 9 | Audit cell execution logging | ⬜ |

## Wave 5 - Frontend Features (PENDING)
| Item | Description | Status |
|------|-------------|--------|
| 40 | Bulk actions on file list | ⬜ |
| 29 | Audit filter improvements | ⬜ |
| 33 | Scalable skill selector | ⬜ |
| 27 | Global agent modal | ⬜ |
| 34 | MCP test button | ⬜ |
| 42 | OIDC test button | ⬜ |

## Wave 6 - Admin & Docs (PENDING)
| Item | Description | Status |
|------|-------------|--------|
| 23 | MOTD system | ⬜ |
| 32 | Import/export .ipynb | ⬜ |
| 11 | Dashboard permissions | ⬜ |
| 21 | OpenAPI docs | ⬜ |

## Database Migrations Needed
- [ ] cell_execution_logs table (Item 13)
- [ ] api_tokens table (Item 22)
- [ ] motd_messages table (Item 23)
- [ ] Remove cells.description column (Item 43)

## Notes
- Items 5, 41 already done
- Items 24, 26 are duplicates of 6, 35
- Item 20 is duplicate of 19
