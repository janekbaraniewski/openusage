# Design: Fix Grid Calculations

## Problem

The `tileGrid()` function in `internal/tui/tiles.go` uses a greedy descending algorithm that tries 3 columns first, then 2, then 1 — returning the first valid fit. This produces unbalanced layouts:

- **4 providers on full screen**: picks 3 columns → 2 rows (3+1), leaving the bottom row with a single lonely tile. A 2x2 grid would be perfectly balanced.
- **8 providers**: picks 3 columns → 3 rows (3+3+2), when 2x4 (if it fits) would have zero empty cells.

The algorithm also hurts keyboard navigation — `down` from column 2 or 3 on the first row may land out-of-bounds when the last row has fewer tiles.

## Root Cause

```go
for c := maxCols; c >= 1; c-- {  // descending: tries max cols first
    // ... validation ...
    return c, perCol, perRowContentH  // returns immediately on first valid
}
```

The loop never compares multiple valid layouts. It optimizes for "most columns that fit" rather than "most balanced layout."

## Solution

Replace the greedy descending loop with a **best-of-all-valid** approach:

1. Iterate all candidate column counts (1 to maxCols)
2. Check width and height constraints for each (same checks as today)
3. Score each valid layout by **empty cell count** (fewer is better)
4. Break ties by preferring **more columns** (more compact layout)
5. Return the best-scored layout

### Scoring

```
empty_cells = (rows * cols) - n
```

- Primary: minimize `empty_cells`
- Secondary (tie-break): maximize `cols` (prefer wider/compact layouts)

### Examples

| n | Current (greedy) | New (balanced) | Improvement |
|---|---|---|---|
| 4 | 3 cols (3+1, 2 empty) | 2 cols (2+2, 0 empty) | Balanced grid |
| 5 | 3 cols (3+2, 1 empty) | 3 cols (3+2, 1 empty) | Same (already optimal) |
| 6 | 3 cols (3+3, 0 empty) | 3 cols (3+3, 0 empty) | Same (already optimal) |
| 7 | 3 cols (3+3+1, 2 empty) | 2 cols (2+2+2+1, 1 empty)* | Better balance |
| 8 | 3 cols (3+3+2, 1 empty) | 2 cols (2+2+2+2, 0 empty)* | Perfect grid |
| 9 | 3 cols (3+3+3, 0 empty) | 3 cols (3+3+3, 0 empty) | Same (already optimal) |

*If height permits; otherwise falls back to 3 columns.

## Implementation Tasks

### Task 1: Refactor `tileGrid()` to evaluate all valid column counts

**File**: `internal/tui/tiles.go`

Replace the `for c := maxCols; c >= 1; c--` loop. Instead:
- Collect all valid (cols, tileW, tileMaxHeight) tuples
- Pick the one with minimum empty cells, breaking ties by maximum cols
- Preserve all existing constraint checks (min width, min multi-column width, min height)

### Task 2: Add comprehensive tests for `tileGrid()`

**File**: `internal/tui/tiles_grid_test.go` (new)

Test cases:
- n=4 wide screen → expects 2 cols (the core bug fix)
- n=5 wide screen → expects 3 cols (already optimal)
- n=6 wide screen → expects 3 cols
- n=1, n=2, n=3 → correct behavior
- Narrow screen forcing single column
- Height-constrained scenarios
- n=0 → edge case

### Task 3: Verify keyboard navigation works with new layouts

No code changes expected — `handleTilesKey` already uses `tileCols()` which delegates to `tileGrid()`. But verify that the new balanced layouts improve navigation (e.g., down from col 2 with 4 items in 2x2 grid lands on the correct tile).

## Non-Goals

- No external dependencies
- No changes to tile rendering, just the grid dimension calculation
- No changes to constants (min widths, gaps, etc.)
