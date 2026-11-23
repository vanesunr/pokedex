// Package jsonfile reads the raw pokemons.json data set into domain objects.
package jsonfile

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pokedex/internal/domain"
)

// rawRange is a min/max pair as written in the JSON, with the unit attached
// to the text (e.g. "6.04kg", "0.79m").
type rawRange struct {
	Minimum string `json:"minimum"`
	Maximum string `json:"maximum"`
}

// rawAttack is an attack as written in the JSON.
type rawAttack struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Damage int    `json:"damage"`
}

// rawRef is a reference to another pokemon in an evolution chain.
type rawRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// rawPokemon mirrors the JSON document, quirks included: the optional
// class / capture area information is stored as ad-hoc keys.
type rawPokemon struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Classification string   `json:"classification"`
	Types          []string `json:"types"`
	Resistant      []string `json:"resistant"`
	Weaknesses     []string `json:"weaknesses"`
	Weight         rawRange `json:"weight"`
	Height         rawRange `json:"height"`
	FleeRate       float64  `json:"fleeRate"`
	MaxCP          int      `json:"maxCP"`
	MaxHP          int      `json:"maxHP"`
	Attacks        struct {
		Fast    []rawAttack `json:"fast"`
		Special []rawAttack `json:"special"`
	} `json:"attacks"`
	EvolutionRequirements *struct {
		Amount int    `json:"amount"`
		Name   string `json:"name"`
	} `json:"evolutionRequirements"`
	Evolutions         []rawRef `json:"evolutions"`
	PreviousEvolutions []rawRef `json:"Previous evolution(s)"`
	PokemonClass       string   `json:"Pokémon Class"`
	CommonCaptureArea  string   `json:"Common Capture Area"`
}

// Parse decodes the contents of pokemons.json.
func Parse(r io.Reader) ([]domain.Pokemon, error) {
	var raw []rawPokemon
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode pokemons: %w", err)
	}
	out := make([]domain.Pokemon, 0, len(raw))
	for _, rp := range raw {
		p, err := rp.toDomain()
		if err != nil {
			return nil, fmt.Errorf("pokemon %q: %w", rp.ID, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// toDomain converts the raw JSON record into a domain.Pokemon, parsing the
// id and the weight/height strings and cleaning up the class and capture area.
func (rp rawPokemon) toDomain() (domain.Pokemon, error) {
	id, err := strconv.Atoi(rp.ID)
	if err != nil {
		return domain.Pokemon{}, fmt.Errorf("invalid id: %w", err)
	}
	weight, err := toRange(rp.Weight, "kg")
	if err != nil {
		return domain.Pokemon{}, fmt.Errorf("weight: %w", err)
	}
	height, err := toRange(rp.Height, "m")
	if err != nil {
		return domain.Pokemon{}, fmt.Errorf("height: %w", err)
	}

	p := domain.Pokemon{
		ID:                 id,
		Name:               rp.Name,
		Classification:     rp.Classification,
		Types:              rp.Types,
		Resistant:          rp.Resistant,
		Weaknesses:         rp.Weaknesses,
		Weight:             weight,
		Height:             height,
		FleeRate:           rp.FleeRate,
		MaxCP:              rp.MaxCP,
		MaxHP:              rp.MaxHP,
		Class:              parseClass(rp.PokemonClass),
		CaptureArea:        strings.TrimSpace(strings.TrimPrefix(rp.CommonCaptureArea, "Early reports that this Pokémon is likely to be found in:")),
		Attacks:            domain.Attacks{Fast: toAttacks(rp.Attacks.Fast), Special: toAttacks(rp.Attacks.Special)},
		Evolutions:         toRefs(rp.Evolutions),
		PreviousEvolutions: toRefs(rp.PreviousEvolutions),
	}
	if rp.EvolutionRequirements != nil {
		p.EvolutionRequirement = &domain.EvolutionRequirement{
			Amount:    rp.EvolutionRequirements.Amount,
			CandyName: rp.EvolutionRequirements.Name,
		}
	}
	return p, nil
}

// toRange converts a rawRange whose values end in unit into a domain.Range.
func toRange(r rawRange, unit string) (domain.Range, error) {
	lo, err := parseMeasure(r.Minimum, unit)
	if err != nil {
		return domain.Range{}, err
	}
	hi, err := parseMeasure(r.Maximum, unit)
	if err != nil {
		return domain.Range{}, err
	}
	return domain.Range{Min: lo, Max: hi}, nil
}

// parseMeasure parses a number with an optional unit suffix, e.g. "6.04kg".
func parseMeasure(s, unit string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), unit), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid measure %q: %w", s, err)
	}
	return v, nil
}

// parseClass maps "This is a LEGENDARY Pokémon." to domain.ClassLegendary.
func parseClass(s string) string {
	switch {
	case strings.Contains(s, domain.ClassLegendary):
		return domain.ClassLegendary
	case strings.Contains(s, domain.ClassMythic):
		return domain.ClassMythic
	default:
		return ""
	}
}

// toAttacks converts raw attacks into domain attacks.
func toAttacks(in []rawAttack) []domain.Attack {
	out := make([]domain.Attack, len(in))
	for i, a := range in {
		out[i] = domain.Attack{Name: a.Name, Type: a.Type, Damage: a.Damage}
	}
	return out
}

// toRefs converts raw evolution references into domain references.
func toRefs(in []rawRef) []domain.EvolutionRef {
	out := make([]domain.EvolutionRef, len(in))
	for i, r := range in {
		out[i] = domain.EvolutionRef{ID: r.ID, Name: r.Name}
	}
	return out
}
