## Implemented

- [x] Invite link generation and flow (fixed redirect for new users via sessionStorage)
- [x] Keyboard shortcuts modal displays correctly in dark theme
- [x] Folder tree collapse button positioning and contrast improved
- [x] Dashboards support editing placed widgets (onEdit prop in WidgetCard)
- [x] Run all widgets button on dashboards page
- [x] Widgets have play button with spinning icon while query runs
- [x] Permission profiles removed; using direct role-based access (Admin/Editor/Viewer/No Access)
- [x] Audit page tracks cell execution actions with cell_execution_logs
- [x] Cell execution metrics displayed in footer (connect, query, render, total time)
- [x] Files page defaults to user's home folder
- [x] Dashboard permission system implemented
- [x] Skills usable via /skill: autocomplete in agent chat
- [x] Maximum tool turns configurable per agent (MaxTurns field)
- [x] OpenAPI/Swagger documentation available at /swagger
- [x] Personal access tokens with configurable expiration
- [x] MOTD admin configuration with dismissable banners
- [x] App-styled confirmation dialogs (replaced OS confirm())
- [x] Members and Groups have different sidebar icons (UserCircle vs Users)
- [x] Audit page filters work for action, user, resource_type
- [x] Subagent spawning implemented for agent chats
- [x] Notebook content tool for agents (GetNotebookContext with max_cells safeguard)
- [x] Import/export notebooks with .ipynb compatibility
- [x] Drag-and-drop reordering of cells
- [x] Bulk actions on file list (multi-select, move, delete, permissions)
- [x] Profile status field has character limit indicator (100 chars)
- [x] Yjs as single source of truth for cell content — agent updates via `update_cell` are immediately reflected in the collaborative editor without being reverted by auto-save

## Not implemented

- [x] The predefined "profiles" for permissions are not working in any resource, remove it entirely
- [ ] There should be a /mcp server that lists available mcp servers. We'll have to think about how to implement authentication here aswell. Will it be individual by user via Oauth, configured with api token in the mcp config?
- [x] For images in markdown cells, there should be a button in the middle of them to open in full screen and have configurable zoom in this view
- [x] In the notebooks page there should be buttons to hide all code, hide all outputs, and the show equivalents for them. — Implemented "Hide Code" and "Hide Outputs" toggle buttons in the notebook toolbar
- [x] Markdown cells should have a configuration (with given security disclaimer) to enable rendering of HTML directly, e.g., embedding HTMLs from other systems
- [x] It should be possible to trigger the agent modal from outside of notebooks. The use case would be actions that are not specific to only one notebook, e.g., analyzing all notebooks in a given folder, listing how many notebooks were created in a given period, help finding a given notebook etc
- [x] The mcp config should have a test button to validate the connection to the mcp is successful. If the mcp connects via Oauth, it should connect with the logged user account to do so. — Implemented test button for HTTP MCP servers (stdio not supported)
- [x] It is possible to select the cell (which makes the output value appear in a small right modal) of multiple output cells. With that, if you have more than one cell selected, pressing arrow keys will move the selected result on all the output cells, which is not the desired behavior. It should be possible to have a selection on only one cell at a time.
- [x] When clicking to create a new code cell and the button to create code cell is already near the bottom of the page, the new cell is created visually "cut" by the bottom. When creating a new cell, is should slight scroll up so it is all visible up to the create cell buttons
- [ ] There is certain situation (which needs deeper troubleshooting) where typing into new code cells gets pretty slow. Typing is the only thing that is slow, navigating the page, scrolling through results, clicking buttons is all good, but typing, which takes a good 2 seconds to appear after a character is typed. It may have something to do with lots of rows returned, in this particular scenario I had 1000 results with 20 columns displayed when the slow-down happened (from the clickhouse connector, cloudtrail_events table). Truncating the row number to 10 made typing be fast again.
- [x] **Text cell editor doesn't show markdown preview while editing** — Text cells use a plain editor without live markdown preview. Users have to click away to see the rendered output. A split-pane or inline preview would improve the experience.
- [x] **OIDC provider form has no "Test" or "Validate" button** — When adding an OIDC provider in Settings, there's no way to test the configuration before saving. Users have to save and then try logging in to verify it works. — Implemented test button that validates the discovery URL
- [x] **Notebook description field doesn't support markdown** — The notebook description is a plain text input. Users might expect to format it with markdown since the rest of the app supports it.
- [x] **Connector schema browser table allowlist/denylist** — Added regex-based table filtering to connectors. Configure which tables appear in the schema browser using allowlist/denylist patterns (one regex per line). Denylist takes precedence.
