# Improvements Backlog

This file tracks known bugs, UX gaps, and feature improvements for future sprints.

## Bugs

- There are components in Storybook that are being tested but are not currently used in the application. Delete them and their specific tests.

## UX Improvements

- [ ] The Groups page in the dark theme has no contrast for the cursor nor the text in the create new group input text
- [ ] The "add member" dropdown uses the "system" dropdown. It should be a stylized one that matches the style of the app and not be system native, but rather web based completely
- [ ] The cursor on the dark theme for code cells is still with wrong contrast. You should validate it directly before saying it is fixed.
- [ ] "SHOW TABLES" is not color highlighted in a code cell.
- [ ] Color highlighting should be able to identify the type of connection inside the cell and use appropriate syntax understanding
- [ ] On the table results, it should be possible to click on the column names to order by it. It should respect the type of the column, if string alphabetically, if number smaller/greater. Order by should be NONE, ASC, DESC on these clicks
- [ ] Some column values are too big to directly show in the table view. When that's the case, there should be some default truncation, but the value should be clickable. When clicking, a new modal should appear on the right side of the cell or page showing the full value. This "navigation" should support moving from the keyboard, so pressing Down would select the next row and the focused value change with it

## Features

- [ ] I may be mistaken, but while there is a button to login with SSO/OIDC, there is no way to configure it. Is that reading correct? *(confirmed: OIDC providers are startup-configured only via env/config; runtime UI configuration requires a new admin panel + hot-reload — pending)*
- [ ] Trying to run a query into a clickhouse query returns: scan: clickhouse [ScanRow]: (user_id) converting UInt32 to *uint64 is unsupported. try using *uint32; Query: SELECT * FROM test.events LIMIT 100;