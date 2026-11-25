## ADDED Requirements

### Requirement: Sidebar Groups section folds

The sidebar Groups section heading SHALL be a disclosure control that exposes its expanded state and folds and unfolds its group entries by pointer and by keyboard. The Groups section SHALL start expanded when the window is 720 px tall or taller and collapsed when it is shorter. A collapsed Groups section SHALL keep its heading and its group entries in the document.

#### Scenario: Sidebar Groups start collapsed in short windows

- **WHEN** the window is shorter than 720 px and the sidebar shows groups
- **THEN** the Groups heading MUST expose itself as collapsed and its group entries MUST be hidden
- **AND** activating the Groups heading MUST expand the group entries
