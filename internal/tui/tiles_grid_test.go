package tui

import "testing"

func TestTileGrid(t *testing.T) {
	m := Model{}

	// Wide screen: plenty of room for 3 columns.
	// tileMinMultiColumnWidth=62, tileBorderH=2, tileGapH=2
	// For 3 cols: perCol = (198 - 4)/3 - 2 = 62  (just meets multi-col min)
	// For 2 cols: perCol = (198 - 2)/2 - 2 = 96
	const wideW = 200
	const tallH = 50

	tests := []struct {
		name     string
		w, h, n  int
		wantCols int
	}{
		{
			name: "n=0 returns 1 col",
			w:    wideW, h: tallH, n: 0,
			wantCols: 1,
		},
		{
			name: "n=1 returns 1 col",
			w:    wideW, h: tallH, n: 1,
			wantCols: 1,
		},
		{
			name: "n=2 returns 2 cols (perfect 1x2)",
			w:    wideW, h: tallH, n: 2,
			wantCols: 2,
		},
		{
			name: "n=3 returns 3 cols (perfect 1x3)",
			w:    wideW, h: tallH, n: 3,
			wantCols: 3,
		},
		{
			name: "n=4 returns 2 cols (balanced 2x2, not 3+1)",
			w:    wideW, h: tallH, n: 4,
			wantCols: 2,
		},
		{
			name: "n=5 returns 3 cols (3+2 better than 2+2+1)",
			w:    wideW, h: tallH, n: 5,
			wantCols: 3,
		},
		{
			name: "n=6 returns 3 cols (perfect 2x3)",
			w:    wideW, h: tallH, n: 6,
			wantCols: 3,
		},
		{
			name: "n=7 prefers fewer empty cells",
			w:    wideW, h: tallH, n: 7,
			// 3 cols: 3 rows (3+3+1), 2 empty
			// 2 cols: 4 rows (2+2+2+1), 1 empty → better balance
			wantCols: 2,
		},
		{
			name: "n=8 prefers 2 cols (perfect 4x2)",
			w:    wideW, h: tallH, n: 8,
			// 3 cols: 3 rows (3+3+2), 1 empty
			// 2 cols: 4 rows (2+2+2+2), 0 empty → perfect balance
			wantCols: 2,
		},
		{
			name: "n=9 returns 3 cols (perfect 3x3)",
			w:    wideW, h: tallH, n: 9,
			wantCols: 3,
		},
		{
			name: "narrow screen forces single column",
			// Width only fits 1 column (too narrow for tileMinMultiColumnWidth)
			w: 70, h: tallH, n: 4,
			wantCols: 1,
		},
		{
			name: "medium screen fits 2 cols but not 3",
			// For 2 cols: perCol = (148 - 2)/2 - 2 = 71 >= 62 ✓
			// For 3 cols: perCol = (148 - 4)/3 - 2 = 46 < 62 ✗
			w: 150, h: tallH, n: 4,
			wantCols: 2,
		},
		{
			name: "very short height may reduce rows",
			// With limited height, more columns (fewer rows) may be forced.
			// tileMinHeight=7, tileBorderV=2, tileGapV=1
			// 2 cols for n=4: 2 rows → need 2*(7+2)+1 = 19 lines
			// 3 cols for n=4: 2 rows → same height requirement
			// At h=18, 2 rows doesn't fit for multi-col:
			//   usableH = 18 - 1 = 17, perRow = 17/2 - 2 = 6.5 → 6 < 7
			// But 3 cols with 2 rows has same problem. Falls to 1 col.
			w: wideW, h: 18, n: 4,
			wantCols: 1,
		},
		{
			name: "height just enough for 2 rows",
			// 2 rows: need usableH/2 - 2 >= 7 → usableH >= 18 → contentH - 1 >= 18 → contentH >= 19
			w: wideW, h: 19, n: 4,
			wantCols: 2,
		},
		{
			name: "zero width uses fallback",
			w:    0, h: tallH, n: 3,
			wantCols: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, tileW, _ := m.tileGrid(tt.w, tt.h, tt.n)
			if cols != tt.wantCols {
				t.Errorf("tileGrid(%d, %d, %d): got cols=%d, want cols=%d (tileW=%d)",
					tt.w, tt.h, tt.n, cols, tt.wantCols, tileW)
			}
			if tileW < tileMinWidth {
				t.Errorf("tileGrid(%d, %d, %d): tileW=%d < tileMinWidth=%d",
					tt.w, tt.h, tt.n, tileW, tileMinWidth)
			}
		})
	}
}

func TestTileGridBalanceProperty(t *testing.T) {
	// Property: for any n and sufficient screen size, the chosen multi-column
	// layout should have the minimum possible empty cells among all valid
	// multi-column options. Single column is a scrollable fallback and doesn't
	// compete on empty cell count.
	m := Model{}

	for n := 2; n <= 12; n++ {
		cols, _, _ := m.tileGrid(200, 100, n)
		if cols < 2 {
			continue // single column fallback, skip balance check
		}
		rows := (n + cols - 1) / cols
		empty := rows*cols - n

		// Check that no other multi-column count produces fewer empty cells.
		for c := 2; c <= tileMaxColumns && c <= n; c++ {
			r := (n + c - 1) / c
			e := r*c - n
			if e < empty {
				t.Errorf("n=%d: chose %d cols (%d empty) but %d cols would have %d empty",
					n, cols, empty, c, e)
			}
		}
	}
}
