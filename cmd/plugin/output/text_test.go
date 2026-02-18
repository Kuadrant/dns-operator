//go:build unit

package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTextFormatter() (*TextOutputFormatter, *bytes.Buffer) {
	var buf bytes.Buffer
	f := &TextOutputFormatter{w: &buf}
	return f, &buf
}

func TestTextPrint(t *testing.T) {
	f, buf := newTextFormatter()
	f.Print("cat")

	got := buf.String()
	if got != "cat\n" {
		t.Errorf("expected %q, got %q", "cat\n", got)
	}
}

func TestTextPrintMultiple(t *testing.T) {
	f, buf := newTextFormatter()
	f.Print("cat")
	f.Print("another cat")
	f.Print("more cats")

	got := buf.String()
	expected := "cat\nanother cat\nmore cats\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestTextError(t *testing.T) {
	f, buf := newTextFormatter()
	f.Error(errors.New("oops"), "cat error")

	got := buf.String()
	expected := "cat error: oops\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestTextPrintObject(t *testing.T) {
	for _, tc := range testCats {
		t.Run(tc.Name, func(t *testing.T) {
			f, buf := newTextFormatter()
			f.PrintObject(tc)

			got := buf.String()

			// Text formatter marshals structs as YAML
			if !strings.Contains(got, "name: "+tc.Name) {
				t.Errorf("expected output to contain 'name: %s', got: %q", tc.Name, got)
			}
		})
	}
}

func TestTextPrintTable(t *testing.T) {
	f, buf := newTextFormatter()
	f.PrintTable(testTable)

	got := buf.String()
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")

	// First line should be headers
	if len(lines) < 1 {
		t.Fatal("expected at least a header line")
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "cuteness") {
		t.Errorf("expected header line to contain column names, got: %q", lines[0])
	}

	// Should have header + one row per cat
	expectedLines := len(testCats) + 1
	if len(lines) != expectedLines {
		t.Fatalf("expected %d lines (1 header + %d rows), got %d", expectedLines, len(testCats), len(lines))
	}

	// Each data row should contain the corresponding cat's name
	for i, tc := range testCats {
		if !strings.Contains(lines[i+1], tc.Name) {
			t.Errorf("expected row %d to contain %q, got: %q", i, tc.Name, lines[i+1])
		}
	}
}

func TestTextPrintTableMismatchedColumns(t *testing.T) {
	f, buf := newTextFormatter()
	table := PrintableTable{
		Headers: []string{"name", "cuteness"},
		Data: [][]string{
			{"Suzanne", "10", "extra"},
		},
	}
	f.PrintTable(table)

	got := buf.String()
	if !strings.Contains(got, "Can't print table") {
		t.Errorf("expected error message about mismatched columns, got: %q", got)
	}
}

func TestTextFullSequence(t *testing.T) {
	f, buf := newTextFormatter()

	f.Print("cat")
	f.Print("another cat")
	f.Print("more cats")
	f.Error(errors.New("oops"), "cat error")
	f.PrintObject(testSuzanne)
	f.Print("another cat")
	f.Print("more cats")
	f.PrintTable(testTable)

	got := buf.String()

	// Verify key elements are present in order
	elements := []string{
		"cat\n",
		"another cat\n",
		"more cats\n",
		"cat error: oops\n",
		"name: Suzanne",
		"another cat\n",
		"more cats\n",
		"Suzanne",
	}

	lastIdx := 0
	for _, elem := range elements {
		idx := strings.Index(got[lastIdx:], elem)
		if idx == -1 {
			t.Errorf("expected output to contain %q after position %d\nfull output:\n%s", elem, lastIdx, got)
		}
		lastIdx += idx + len(elem)
	}
}
