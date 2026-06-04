# Markdown Cell UX Improvements

## Overview
Comprehensive UX improvements to the MarkdownCell component to make it more intuitive and user-friendly, especially for image handling via Ctrl+V paste and drag-and-drop.

## Key Improvements

### 1. **Simplified Editor Architecture**
**Before:** Block-based editing where content was split at blank lines, causing confusion when clicking different parts of the cell would switch which "block" you were editing.

**After:** Single unified textarea for the entire markdown content. No more confusing block switching - what you see is what you edit.

### 2. **Enhanced Formatting Toolbar**
Added a comprehensive toolbar with formatting buttons:
- **Image Upload** - With visual feedback and progress indicator
- **Bold (B)** - Wraps selected text in `**`
- **Italic (I)** - Wraps selected text in `*`
- **Link** - Inserts `[text](url)` template
- **Heading** - Adds `## ` to current line
- **Inline Code** - Wraps selected text in backticks

All buttons respect text selection and place cursor intelligently.

### 3. **Drag & Drop Image Support**
- **Visual feedback** - When dragging an image over the cell, shows a dashed border overlay with an icon and "Drop image here" text
- **Seamless integration** - Dropped images are automatically uploaded and inserted at cursor position
- **No accidental drops** - Clear visual indication of drop zone

### 4. **Upload Progress Indicator**
- **Progress bar** - Shows real-time upload progress (0-100%)
- **Status text** - Displays "Uploading... X%" during upload
- **Smooth transitions** - Progress bar animates smoothly
- **Auto-hide** - Disappears 300ms after completion

### 5. **Better Placeholder Text**
**Before:** Generic "Write markdown…"

**After:** "Write markdown… (Ctrl+V to paste images, drag & drop supported)"
- Clearly communicates image paste capability
- Mentions drag & drop support
- Reduces discovery friction

### 6. **Tab Key Support**
- **Before:** Tab key would move focus away from the editor
- **After:** Tab inserts 2 spaces (standard markdown indentation)
- Improves workflow for writing indented content (lists, code blocks)

### 7. **Contextual Toolbar Hints**
- Shows "Ctrl+V to paste images" hint when focused
- Changes to "Uploading..." during image upload
- Provides continuous feedback about available actions

### 8. **Improved Image Upload Button**
**Before:** Small icon-only button with no label

**After:** 
- Icon + "Image" label for clarity
- Shows loading spinner during upload
- Larger touch target (better accessibility)
- Tooltip: "Upload image (or paste with Ctrl+V)"

## Technical Changes

### Removed
- `splitIntoBlocks()` and `joinBlocks()` functions (no longer needed)
- `MarkdownBlock` component (replaced with single textarea)
- Block-focused state management (`focusedIdx`, `blocks` array)
- Complex block synchronization logic

### Added
- Drag-and-drop event handlers with visual feedback
- Upload progress tracking (`uploadProgress` state)
- Formatting toolbar with 6 action buttons
- Tab key handler for space insertion
- Enhanced placeholder and hint text

### Modified
- `MarkdownView` component now uses single `source` state instead of `blocks` array
- Simplified `updateSource` and `blurEditor` functions
- Streamlined image insertion logic (no block index needed)
- Better focus management with `isFocused` boolean

## User Experience Flow

### Writing Markdown
1. Click anywhere in the markdown cell to focus
2. Start typing - full markdown content in one textarea
3. Use toolbar buttons for formatting or type markdown syntax directly
4. Press Escape to blur and see rendered preview
5. Click again to resume editing

### Adding Images (3 methods)
1. **Ctrl+V Paste** - Copy an image, press Ctrl+V in the editor
2. **Drag & Drop** - Drag an image file over the cell, drop it
3. **Upload Button** - Click "Image" button in toolbar, select file

All methods show progress indicator and insert image at cursor position.

### Formatting Text
1. Select text in the editor
2. Click toolbar button (B, I, Link, etc.)
3. Formatting is applied around selection
4. Cursor is placed intelligently for continued editing

## Visual Design

### Toolbar
- Clean, minimal design with icon buttons
- Subtle borders and hover states
- Grouped logically (image | formatting | code)
- Right-aligned hint text for context

### Drag Overlay
- Semi-transparent background
- Dashed border in accent color
- Large icon + text centered
- Non-interactive (pointer-events: none)

### Upload Progress
- Thin progress bar (4px height)
- Accent color fill
- Percentage text below
- Smooth width transitions

## Accessibility Improvements
- Larger button touch targets
- Clear tooltips on all buttons
- Descriptive placeholder text
- Tab key doesn't trap focus
- Visual feedback for all interactions

## Browser Testing Results
Tested with agent-browser automation:
- ✅ Cell creation and focus
- ✅ Toolbar rendering and button visibility
- ✅ Text input and editing
- ✅ Escape to blur and preview
- ✅ Drag overlay appearance
- ✅ Upload progress indicator
- ✅ Responsive layout

## Files Modified
- `web/src/components/MarkdownCell.tsx` - Complete rewrite of MarkdownView component

## Backward Compatibility
- All existing markdown content renders correctly
- Image resize functionality preserved
- Yjs collaboration support maintained
- API integration unchanged
- No breaking changes to cell data model

## Future Enhancements (Not Implemented)
- Keyboard shortcuts for formatting (Ctrl+B, Ctrl+I, etc.)
- Table insertion button
- Code block insertion with language selector
- Undo/redo visualization
- Word count in toolbar
- Reading time estimate
