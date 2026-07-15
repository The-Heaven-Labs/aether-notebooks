# Requirements: dashboard-view (MODIFIED)

## MODIFIED: Widget grid layout preserves author positions

Widgets on the dashboard view page SHALL render at their stored `row`/`col` positions without any automatic compaction or reordering.

### Scenarios

**WHEN** a dashboard has widgets at known grid positions
**AND** the view page renders the `GridLayout`
**THEN** widgets SHALL appear at their exact stored positions
**AND** no widget SHALL overlap another widget
**AND** empty grid space SHALL remain empty (no upward compaction)
