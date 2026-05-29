## Implemented

- [x] If I'm a viewer and access one notebook, I can start editing it but when leaving I receive insufficient permissions. If the user has no permission, cells should be read-only. Same goes for the permissions menu, I can start editing and even save permissions for a notebook I have no permission, but it should be blocked from even starting
- [x] In the groups page, there is no feedback if I do not fill the new group name and press the create button. Same trying to add someone to a group but not selecting a person
- [x] On the settings page, pressing the Save button has no feedback
- [x] In the connectors page, if I click test it will update the status, but clicking again shows no feedback and nothing changes
- [x] When logging-in, if the user does not authenticate with SSO, the password field should have focus
- [x] The settings page flashes quickly on load then turns all white
- [x] Contrast of the left menu items on the light theme with the dark lateral menu background color
- [x] Contrast of the cursor in code cells in the dark theme is not good. Cursor is dark on the dark background.
- [x] Code cells should have a default LIMIT 1000 config that is applied to queries. It should be visible in the UI and have a Unlimited option
- [x] Contrast for color highlighting of code cells in the dark theme are not great, I think it misses saturation for the colors, look too much like white
- [x] Add a default No Access role to go along admin/editor/viewer
- [x] In the audit view, actions made to notebooks show the notebook name and the first part of the id. While a tooltip appears showing the full id, there is no way to copy the full id.
- [x] Adding an user to a group does not create an audit action same for removing
- [x] Removing an user from a group does not ask for confirmation on the UI
- [x] When creating an account, I can either create a new org or paste an invite token. But it seems there is no way to generate such token?
- [x] The navigation for items created should have a "home" and from this home, every user home folder should be in it, and thus it should be possible for users to (given they have access) navigate other users' folders
- [x] Currently there is no way to change permissions on who can access/use/etc on a connector.
- [x] In the agent chat, there is no way to access history for past sessions
- [x] Pressing "/" in the chat should open a picker for the possible slash commands
- [x] Clicking on the AI button should open the chat with a default agent. This default should be configurable
- [x] When the LLM is working, I can't right away send a message to be queued, it is blocked
- [x] When one message is sent and the chat input is blocked, the form goes out of focus, and I need to click it again to type the next message
- [x] There should be native tool calls to list notebooks, connectors, folders and everything the user has access in the platform
- [x] When agent panel is opened, the input form should be focused automatically
- [x] There should be a native tool call for data schema exploration
- [x] There should be native tools for task tracking
- [x] When the agent creates a new cell, it should be highlighted and scrolled immediatly, not only when that part of the session/command ends
- [x] Instead of MCPs being configured at agent level, they should be configured at application level, and then could be shared between multiple agents

## Not Yet Implemented

- [x] There should be a way to copy the whole agent conversation as markdown
