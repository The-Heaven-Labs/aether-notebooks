---
name: Use agent-browser for web testing
description: User wants agent-browser tool used instead of Playwright Python scripts for testing web apps
type: feedback
---

Use the `agent-browser` MCP tool for browser/web testing tasks, not custom Python Playwright scripts.

**Why:** User explicitly prefers agent-browser over writing custom playwright scripts.

**How to apply:** When testing or inspecting a local web app, use agent-browser MCP tool. If it's not available in the current session, ask the user to configure it rather than falling back to Python Playwright scripts.
