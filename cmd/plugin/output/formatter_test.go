//go:build unit

package output

import (
	"sort"
	"strconv"
	"strings"
)

type trait struct {
	Name          string
	WhenDisplayed []string
}

type cat struct {
	Name      string
	Cuteness  int
	Traits    []trait
	Relatives map[string]string
}

var testSuzanne = cat{
	Name:     "Suzanne",
	Cuteness: 10,
	Traits: []trait{
		{
			Name:          "cuddly",
			WhenDisplayed: []string{"morning", "afternoon: 13:05 exactly"},
		},
		{
			Name:          "sleepy",
			WhenDisplayed: []string{"every, single, time"},
		},
	},
	Relatives: map[string]string{
		"mother":  "Joane",
		"father":  "Sam",
		"Brother": "Dingus",
	},
}

var testJoane = cat{
	Name:     "Joane",
	Cuteness: 8,
	Traits: []trait{
		{
			Name:          "cuddly",
			WhenDisplayed: []string{"morning", "afternoon"},
		},
	},
	Relatives: map[string]string{
		"daughter": "Suzanne",
		"son":      "Dingus",
		"husband":  "Sam",
	},
}

var testSam = cat{
	Name:     "Sam",
	Cuteness: 9,
	Traits: []trait{
		{
			Name:          "cuddly",
			WhenDisplayed: []string{"morning", "afternoon"},
		},
	},
	Relatives: map[string]string{
		"daughter": "Suzanne",
		"son":      "Dingus",
		"wife":     "Joane",
	},
}

// testCats is all three cats for use in tests that need the full set
var testCats = []cat{testSuzanne, testJoane, testSam}

func buildTestTable(cats []cat) PrintableTable {
	table := PrintableTable{
		Headers: []string{"name", "cuteness", "traits", "relatives"},
	}
	for _, c := range cats {
		var traitNames []string
		for _, t := range c.Traits {
			traitNames = append(traitNames, t.Name)
		}

		keys := make([]string, 0, len(c.Relatives))
		for k := range c.Relatives {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var relativeParts []string
		for _, k := range keys {
			relativeParts = append(relativeParts, k+": "+c.Relatives[k])
		}

		table.Data = append(table.Data, []string{
			c.Name,
			strconv.Itoa(c.Cuteness),
			strings.Join(traitNames, ", "),
			strings.Join(relativeParts, "; "),
		})
	}
	return table
}

var testTable = buildTestTable(testCats)
