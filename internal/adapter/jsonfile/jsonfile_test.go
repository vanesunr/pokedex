// jsonfile_test.go tests the pokemons.json parser against the real data set
// and against malformed input.

package jsonfile

import (
	"os"
	"strings"
	"testing"

	"pokedex/internal/domain"
)

// TestParse_RealDataSet parses the real file and spot-checks the count, unit
// handling, evolution requirement, classes, capture area and evolutions.
func TestParse_RealDataSet(t *testing.T) {
	f, err := os.Open("../../../pokemons.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 151 {
		t.Fatalf("got %d pokemons, want 151", len(got))
	}

	byID := map[int]domain.Pokemon{}
	for _, p := range got {
		byID[p.ID] = p
	}
	if byID[1].Name != "Bulbasaur" || byID[1].Weight.Min != 6.04 || byID[1].Height.Max != 0.79 {
		t.Errorf("unexpected Bulbasaur: %+v", byID[1])
	}
	if r := byID[1].EvolutionRequirement; r == nil || r.Amount != 25 || r.CandyName != "Bulbasaur candies" {
		t.Errorf("unexpected evolution requirement: %+v", r)
	}
	if byID[150].Class != domain.ClassLegendary || byID[151].Class != domain.ClassMythic {
		t.Errorf("classes: Mewtwo=%q Mew=%q", byID[150].Class, byID[151].Class)
	}
	if byID[83].CaptureArea != "Asia" {
		t.Errorf("Farfetch'd capture area = %q", byID[83].CaptureArea)
	}
	if len(byID[2].PreviousEvolutions) != 1 || len(byID[1].Evolutions) != 2 {
		t.Errorf("evolutions not parsed")
	}
}

// TestParse_Errors checks that malformed JSON, ids and measures return errors.
func TestParse_Errors(t *testing.T) {
	tests := map[string]string{
		"bad json":   `{`,
		"bad id":     `[{"id":"x"}]`,
		"bad weight": `[{"id":"1","weight":{"minimum":"abc","maximum":"1kg"},"height":{"minimum":"1m","maximum":"1m"}}]`,
		"bad height": `[{"id":"1","weight":{"minimum":"1kg","maximum":"1kg"},"height":{"minimum":"1m","maximum":"?"}}]`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(in)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
