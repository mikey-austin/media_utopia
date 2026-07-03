package output

import (
	"strings"
	"testing"
)

func plainTable() Table {
	return Table{
		Columns: []Column{
			{Title: "NAME", Min: 6, Flex: 2},
			{Title: "LEN", Align: AlignRight},
			{Title: "ID"},
		},
		Rows: [][]string{
			{"Song One", "3:45", "abc123"},
			{"A Very Long Song Name That Keeps Going", "12:03", "deadbeef01"},
		},
	}
}

func TestTableLayoutFitsAndAligns(t *testing.T) {
	SetColorEnabled(false)
	tbl := plainTable()
	tbl.Width = 100
	out := tbl.Render()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	// Header present, no vertical separators, two-space gutters.
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("header missing: %q", lines[0])
	}
	if strings.Contains(out, "|") {
		t.Fatalf("vertical separators must not be used:\n%s", out)
	}
	// LEN is right-aligned: "3:45" and "12:03" end in the same column.
	col := strings.Index(lines[2], "12:03") + len("12:03")
	if lines[1][col-len(" 3:45"):col] != " 3:45" {
		t.Fatalf("LEN not right-aligned:\n%s", out)
	}
	// No trailing whitespace on any line.
	for i, l := range lines {
		if strings.TrimRight(l, " ") != l {
			t.Fatalf("line %d has trailing whitespace: %q", i, l)
		}
	}
}

func TestTableShrinksFlexNeverIDs(t *testing.T) {
	SetColorEnabled(false)
	tbl := plainTable()
	tbl.Width = 40
	out := tbl.Render()
	if !strings.Contains(out, "…") {
		t.Fatalf("expected flexible column truncation:\n%s", out)
	}
	// Rigid columns (Flex 0) keep full content for copy-paste.
	if !strings.Contains(out, "deadbeef01") {
		t.Fatalf("rigid ID column must never be truncated:\n%s", out)
	}
	if !strings.Contains(out, "12:03") {
		t.Fatalf("duration column must never be truncated:\n%s", out)
	}
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := displayWidth(l); w > 40 {
			t.Fatalf("line exceeds width budget (%d > 40): %q", w, l)
		}
	}
}

func TestTableColorDiscipline(t *testing.T) {
	SetColorEnabled(false)
	tbl := plainTable()
	tbl.Columns[2].Style = Dim
	tbl.Width = 100
	if out := tbl.Render(); strings.Contains(out, "\x1b[") {
		t.Fatalf("colors disabled but ANSI emitted:\n%q", out)
	}
	SetColorEnabled(true)
	defer SetColorEnabled(false)
	out := tbl.Render()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("colors enabled but no ANSI emitted:\n%q", out)
	}
	// Styled cells must still align: strip ANSI and check width budget.
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := displayWidth(stripANSI(l)); w > 100 {
			t.Fatalf("styled line exceeds width: %q", l)
		}
	}
}

func TestTableSanitizesCells(t *testing.T) {
	SetColorEnabled(false)
	tbl := Table{
		Columns: []Column{{Title: "A", Flex: 1}, {Title: "B"}},
		Rows:    [][]string{{"multi\nline\tcell", "pipe|kept"}},
		Width:   60,
	}
	out := tbl.Render()
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("cell newlines must be flattened:\n%q", out)
	}
	if !strings.Contains(out, "pipe|kept") {
		t.Fatalf("pipe characters must survive (no separator to escape):\n%s", out)
	}
}

func TestDetailsAlignment(t *testing.T) {
	SetColorEnabled(false)
	out := RenderDetails([][2]string{
		{"Renderer", "MPV 1"},
		{"Session", "abc"},
		{"Expires", "in 4m59s"},
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines:\n%s", out)
	}
	// Values start at the same column.
	idx := strings.Index(lines[0], "MPV 1")
	for _, want := range []string{"abc", "in 4m59s"} {
		found := false
		for _, l := range lines {
			if strings.Index(l, want) == idx {
				found = true
			}
		}
		if !found {
			t.Fatalf("value %q not aligned at column %d:\n%s", want, idx, out)
		}
	}
}

func TestFooterFormat(t *testing.T) {
	SetColorEnabled(false)
	if got := Footer(0, 50, 15402); !strings.Contains(got, "1–50 of 15402") {
		t.Fatalf("footer = %q", got)
	}
	if got := Footer(0, 3, 3); got != "" {
		t.Fatalf("complete single page needs no footer, got %q", got)
	}
	if got := Footer(50, 25, 100); !strings.Contains(got, "51–75 of 100") || !strings.Contains(got, "--offset 75") {
		t.Fatalf("footer = %q", got)
	}
}

func TestTableAlignsPreStyledCells(t *testing.T) {
	SetColorEnabled(true)
	defer SetColorEnabled(false)
	tbl := Table{
		Columns: []Column{{Title: "STATE"}, {Title: "NAME"}},
		Rows: [][]string{
			{Green("done"), "alpha"},
			{"failed", "beta"},
		},
		Width: 60,
	}
	out := tbl.Render()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	col := strings.Index(stripANSI(lines[1]), "alpha")
	if strings.Index(stripANSI(lines[2]), "beta") != col {
		t.Fatalf("styled STATE cell broke alignment:\n%s", out)
	}
}
