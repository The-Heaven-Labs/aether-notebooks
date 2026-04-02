# TODO

## Tasks

### 1. Dark Theme Support

Add a dark theme variant and a button to change themes.

**Requirements:**
- Create a dark theme variant of the existing light theme
- Add a theme toggle button (placement TBD)
- Persist theme preference in localStorage
- Apply theme using CSS custom properties

**Files to modify:**
- `web/src/styles/theme.css` - Add dark theme variables
- `web/src/contexts/ThemeContext.tsx` - Create theme context (new)
- `web/src/components/TopBar.tsx` - Add theme toggle button (or elsewhere as decided)

**Effort:** 2-3 hours