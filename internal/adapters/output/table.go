package output

import (
	"fmt"
	"strings"
)

// Align controls horizontal cell alignment.
type Align int

const (
	AlignLeft Align = iota
	AlignRight
)

// Column describes one table column.
type Column struct {
	Title string
	// Min is the smallest width a flexible column may shrink to.
	Min int
	// Flex is the shrink weight when the table exceeds the terminal width.
	// Columns with Flex 0 are rigid: they are never truncated (IDs,
	// durations — anything the user copies).
	Flex int
	// Align controls cell alignment (durations/counts read best right-aligned).
	Align Align
	// Style is applied to each body cell after truncation (e.g. Dim for IDs).
	Style func(string) string
}

// Table renders rows under a terminal-width budget with two-space gutters
// and no vertical separators. Flexible columns absorb any squeeze; rigid
// columns always show full content.
type Table struct {
	Columns []Column
	Rows    [][]string
	// RowStyle optionally styles a whole row (e.g. the current queue entry).
	RowStyle func(row int) func(string) string
	// Width is the layout budget; 0 means the current terminal width.
	Width int
}

const tableGutter = 2

// Render lays the table out and returns it, newline-terminated.
func (t Table) Render() string {
	width := t.Width
	if width <= 0 {
		width = TerminalWidth(120)
	}
	ncols := len(t.Columns)
	if ncols == 0 {
		return ""
	}

	// Natural width per column: max of title and all cells.
	widths := make([]int, ncols)
	for i, c := range t.Columns {
		widths[i] = displayWidth(c.Title)
	}
	rows := make([][]string, len(t.Rows))
	for r, row := range t.Rows {
		cells := make([]string, ncols)
		for i := 0; i < ncols && i < len(row); i++ {
			cell := sanitizeCell(row[i])
			cells[i] = cell
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
		rows[r] = cells
	}

	// Shrink flexible columns (weighted by Flex, floored at Min) until the
	// table fits the width budget.
	total := (ncols - 1) * tableGutter
	for _, w := range widths {
		total += w
	}
	if over := total - width; over > 0 {
		type flexCol struct{ idx, slack, weight int }
		var flex []flexCol
		totalSlack := 0
		for i, c := range t.Columns {
			if c.Flex <= 0 {
				continue
			}
			minW := max(c.Min, displayWidth(c.Title))
			if slack := widths[i] - minW; slack > 0 {
				flex = append(flex, flexCol{idx: i, slack: slack, weight: c.Flex})
				totalSlack += slack
			}
		}
		if totalSlack > 0 {
			cut := min(over, totalSlack)
			remaining := cut
			for n, fc := range flex {
				share := cut * fc.slack * fc.weight / max(1, totalSlack*fc.weight)
				// Last flexible column absorbs rounding.
				if n == len(flex)-1 {
					share = remaining
				}
				share = min(share, fc.slack)
				share = min(share, remaining)
				widths[fc.idx] -= share
				remaining -= share
				if remaining <= 0 {
					break
				}
			}
		}
	}

	var b strings.Builder
	// Header row.
	headerCells := make([]string, ncols)
	for i, c := range t.Columns {
		headerCells[i] = c.Title
	}
	writeRow(&b, headerCells, t.Columns, widths, Header)

	for r, cells := range rows {
		var rowStyle func(string) string
		if t.RowStyle != nil {
			rowStyle = t.RowStyle(r)
		}
		writeRow(&b, cells, t.Columns, widths, rowStyle)
	}
	return b.String()
}

func writeRow(b *strings.Builder, cells []string, cols []Column, widths []int, rowStyle func(string) string) {
	last := len(cols) - 1
	for i, col := range cols {
		cell := cells[i]
		if col.Flex > 0 {
			cell = truncateCell(cell, widths[i])
		}
		pad := widths[i] - displayWidth(cell)
		styled := cell
		if rowStyle != nil {
			styled = rowStyle(cell)
		} else if col.Style != nil {
			styled = col.Style(cell)
		}
		switch {
		case i == last && col.Align == AlignLeft:
			// No trailing padding on the final column.
			b.WriteString(styled)
		case col.Align == AlignRight:
			b.WriteString(strings.Repeat(" ", max(0, pad)))
			b.WriteString(styled)
		default:
			b.WriteString(styled)
			b.WriteString(strings.Repeat(" ", max(0, pad)))
		}
		if i != last {
			b.WriteString(strings.Repeat(" ", tableGutter))
		}
	}
	// Never leave trailing spaces (they show up in pipes and diffs).
	trimTrailingSpaces(b)
	b.WriteString("\n")
}

// trimTrailingSpaces removes trailing spaces from the final line in b.
func trimTrailingSpaces(b *strings.Builder) {
	s := b.String()
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		end--
	}
	if end != len(s) {
		trimmed := s[:end]
		b.Reset()
		b.WriteString(trimmed)
	}
}

func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// RenderDetails prints aligned Label/value pairs (dim labels), the shared
// look for session/resolve/detail views.
func RenderDetails(pairs [][2]string) string {
	labelW := 0
	for _, p := range pairs {
		if w := displayWidth(p[0]); w > labelW {
			labelW = w
		}
	}
	var b strings.Builder
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		label := p[0] + strings.Repeat(" ", labelW-displayWidth(p[0]))
		b.WriteString(Dim(label))
		b.WriteString("  ")
		b.WriteString(p[1])
		b.WriteString("\n")
	}
	return b.String()
}

// Footer renders the standard pagination footer, or "" when the first page
// already contains everything.
func Footer(start, shown, total int64) string {
	if total <= 0 || (start == 0 && shown >= total) {
		return ""
	}
	end := start + shown
	line := fmt.Sprintf("%d–%d of %d", start+1, end, total)
	if end < total {
		line += fmt.Sprintf(" · --offset %d for more", end)
	}
	return Dim(line) + "\n"
}
