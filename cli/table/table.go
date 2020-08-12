package table

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type (
	// Table represents a table of data to be rendered.
	Table struct {
		Columns       []Column
		Data          []Row
		Sort          []int
		ColumnSpacing string
	}

	// Row is a single row of data in a table.
	Row = []string

	// Column represents metadata about a column in a table.
	Column struct {
		Header string
		Width  int
		// If false, render this column.
		Hide bool
		// If true, set the width to the widest value in this column.
		Flexible  bool
		LeftAlign bool
	}
)

const defaultColumnSpacing = "  "

// NewTable creates a new table with the given columns and rows.
func NewTable(cols []Column, data []Row) Table {
	return Table{
		Columns:       cols,
		Data:          data,
		Sort:          []int{},
		ColumnSpacing: defaultColumnSpacing,
	}
}

// Render writes the full table to the given Writer.
func (t *Table) Render(w io.Writer) {
	columnWidths := t.columnWidths()
	t.renderRow(w, t.headerRow(), columnWidths)
	t.sort()
	for _, row := range t.Data {
		t.renderRow(w, row, columnWidths)
	}
}

func (t *Table) columnWidths() []int {
	widths := make([]int, len(t.Columns))
	for c, col := range t.Columns {
		width := col.Width
		if col.Flexible {
			for _, row := range t.Data {
				if len(row[c]) > width {
					width = len(row[c])
				}
			}
		}
		widths[c] = width
	}
	return widths
}

func (t *Table) sort() {
	if len(t.Sort) == 0 {
		return
	}
	sort.Slice(t.Data, func(i, j int) bool {
		for _, sortCol := range t.Sort {
			if t.Data[i][sortCol] < t.Data[j][sortCol] {
				return true
			} else if t.Data[i][sortCol] > t.Data[j][sortCol] {
				return false
			}
		}
		return false
	})
}

func (t *Table) renderRow(w io.Writer, row Row, columnWidths []int) {
	for c, col := range t.Columns {
		if col.Hide {
			continue
		}
		value := row[c]
		if len(value) > columnWidths[c] {
			value = value[:columnWidths[c]]
		}
		padding := strings.Repeat(" ", columnWidths[c]-len(value))
		if col.LeftAlign {
			fmt.Fprintf(w, "%s%s%s", value, padding, t.ColumnSpacing)
		} else {
			fmt.Fprintf(w, "%s%s%s", padding, value, t.ColumnSpacing)
		}
	}
	fmt.Fprint(w, "\n")
}

func (t *Table) headerRow() Row {
	row := make(Row, len(t.Columns))
	for c, col := range t.Columns {
		row[c] = col.Header
	}
	return row
}
