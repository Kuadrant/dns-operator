//go:build unit

package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func newYAMLFormatter() (*YAMLOutputFormatter, *bytes.Buffer) {
	var buf bytes.Buffer
	f := &YAMLOutputFormatter{w: &buf}
	return f, &buf
}

func TestYAMLPrint(t *testing.T) {
	f, buf := newYAMLFormatter()
	f.Print("cat")

	got := buf.String()
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected output to start with file separator, got: %q", got)
	}

	// Verify the message is properly marshalled YAML
	content := strings.TrimPrefix(got, "---\n")
	var parsed map[string]string
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("output after separator is not valid YAML: %v\ncontent: %q", err, content)
	}
	if parsed["message"] != "cat" {
		t.Errorf("expected message 'cat', got %q", parsed["message"])
	}
}

func TestYAMLPrintMultiple(t *testing.T) {
	f, buf := newYAMLFormatter()
	f.Print("cat")
	f.Print("another cat")
	f.Print("more cats")

	got := buf.String()
	documents := strings.Split(got, "---\n")

	// First element is empty (before first ---)
	if documents[0] != "" {
		t.Errorf("expected empty string before first separator, got %q", documents[0])
	}

	expectedMessages := []string{"cat", "another cat", "more cats"}
	for i, expected := range expectedMessages {
		var parsed map[string]string
		if err := yaml.Unmarshal([]byte(documents[i+1]), &parsed); err != nil {
			t.Fatalf("document %d is not valid YAML: %v", i, err)
		}
		if parsed["message"] != expected {
			t.Errorf("document %d: expected message %q, got %q", i, expected, parsed["message"])
		}
	}
}

func TestYAMLError(t *testing.T) {
	f, buf := newYAMLFormatter()
	f.Error(errors.New("oops"), "cat error")

	got := buf.String()
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected output to start with file separator, got: %q", got)
	}

	content := strings.TrimPrefix(got, "---\n")
	var parsed map[string]string
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("output after separator is not valid YAML: %v\ncontent: %q", err, content)
	}
	if parsed["error"] != "oops" {
		t.Errorf("expected error 'oops', got %q", parsed["error"])
	}
	if parsed["message"] != "cat error" {
		t.Errorf("expected message 'cat error', got %q", parsed["message"])
	}
}

func TestYAMLPrintObject(t *testing.T) {
	for _, tc := range testCats {
		t.Run(tc.Name, func(t *testing.T) {
			f, buf := newYAMLFormatter()
			f.PrintObject(tc)

			got := buf.String()
			if !strings.HasPrefix(got, "---\n") {
				t.Errorf("expected output to start with file separator, got: %q", got)
			}

			content := strings.TrimPrefix(got, "---\n")
			var parsed cat
			if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
				t.Fatalf("output after separator is not valid YAML: %v\ncontent: %q", err, content)
			}
			if parsed.Name != tc.Name {
				t.Errorf("expected Name %q, got %q", tc.Name, parsed.Name)
			}
			if parsed.Cuteness != tc.Cuteness {
				t.Errorf("expected Cuteness %d, got %d", tc.Cuteness, parsed.Cuteness)
			}
			if len(parsed.Traits) != len(tc.Traits) {
				t.Errorf("expected %d traits, got %d", len(tc.Traits), len(parsed.Traits))
			}
			if len(parsed.Relatives) != len(tc.Relatives) {
				t.Errorf("expected %d relatives, got %d", len(tc.Relatives), len(parsed.Relatives))
			}
		})
	}
}

func TestYAMLPrintTable(t *testing.T) {
	f, buf := newYAMLFormatter()
	f.PrintTable(testTable)

	got := buf.String()
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected output to start with file separator, got: %q", got)
	}

	content := strings.TrimPrefix(got, "---\n")
	var parsed []map[string]string
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("output after separator is not valid YAML: %v\ncontent: %q", err, content)
	}
	if len(parsed) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(parsed))
	}
	if parsed[0]["name"] != "Suzanne" {
		t.Errorf("expected first row name 'Suzanne', got %q", parsed[0]["name"])
	}
	if parsed[0]["cuteness"] != "10" {
		t.Errorf("expected first row cuteness '10', got %q", parsed[0]["cuteness"])
	}
	if parsed[1]["name"] != "Joane" {
		t.Errorf("expected second row name 'Joane', got %q", parsed[1]["name"])
	}
	if parsed[2]["name"] != "Sam" {
		t.Errorf("expected third row name 'Sam', got %q", parsed[2]["name"])
	}
}

func TestYAMLPrintTableMismatchedColumns(t *testing.T) {
	f, buf := newYAMLFormatter()
	table := PrintableTable{
		Headers: []string{"name", "cuteness"},
		Data: [][]string{
			{"Suzanne", "10", "extra"},
		},
	}
	f.PrintTable(table)

	got := buf.String()
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected output to start with file separator, got: %q", got)
	}
	if !strings.Contains(got, "error printing table") {
		t.Errorf("expected error message about mismatched columns, got: %q", got)
	}
}

func TestYAMLPrintSpecialCharacters(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "colon in message", message: "value: with colon"},
		{name: "quotes in message", message: `contains "quotes"`},
		{name: "comma in message", message: "one, two, three"},
		{name: "hash in message", message: "# not a comment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, buf := newYAMLFormatter()
			f.Print(tt.message)

			got := buf.String()
			content := strings.TrimPrefix(got, "---\n")
			var parsed map[string]string
			if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
				t.Fatalf("output is not valid YAML: %v\ncontent: %q", err, content)
			}
			if parsed["message"] != tt.message {
				t.Errorf("expected message %q, got %q", tt.message, parsed["message"])
			}
		})
	}
}

func TestYAMLFullSequence(t *testing.T) {
	f, buf := newYAMLFormatter()

	f.Print("cat")
	f.Print("another cat")
	f.Print("more cats")
	f.Error(errors.New("oops"), "cat error")
	f.PrintObject(testSuzanne)
	f.Print("another cat")
	f.Print("more cats")
	f.PrintTable(testTable)

	got := buf.String()

	// Every section must start with ---
	documents := strings.Split(got, "---\n")

	// First element is empty (before first ---)
	if documents[0] != "" {
		t.Errorf("expected empty string before first separator, got %q", documents[0])
	}

	// Should have 8 documents (3 prints + 1 error + 1 object + 2 prints + 1 table)
	if len(documents)-1 != 8 {
		t.Fatalf("expected 8 YAML documents, got %d", len(documents)-1)
	}

	// Verify each document is valid YAML
	for i := 1; i < len(documents); i++ {
		var parsed interface{}
		if err := yaml.Unmarshal([]byte(documents[i]), &parsed); err != nil {
			t.Errorf("document %d is not valid YAML: %v\ncontent: %q", i-1, err, documents[i])
		}
	}
}
