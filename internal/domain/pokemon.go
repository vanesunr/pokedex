// Package domain holds the core business types of the Pokedex. It has no
// dependency on any framework, database or transport.
package domain

import (
	"errors"
	"strings"
)

var (
	// ErrNotFound is returned when a requested pokemon does not exist.
	ErrNotFound = errors.New("pokemon not found")
	// ErrInvalidArgument is returned (wrapped) when the caller supplied bad input.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Pokemon classes found in the data set. A regular pokemon has an empty class.
const (
	ClassLegendary = "LEGENDARY"
	ClassMythic    = "MYTHIC"
)

// Range is a min/max pair. Weight is expressed in kg and height in meters.
type Range struct {
	Min float64
	Max float64
}

// Attack is a move a pokemon can learn.
type Attack struct {
	Name   string
	Type   string
	Damage int
}

// Attacks groups a pokemon's moves by category.
type Attacks struct {
	Fast    []Attack
	Special []Attack
}

// EvolutionRef points at another pokemon in an evolution chain.
type EvolutionRef struct {
	ID   int
	Name string
}

// EvolutionRequirement is the candy cost needed to evolve a pokemon.
type EvolutionRequirement struct {
	Amount    int
	CandyName string
}

// Pokemon is the aggregate root of the catalog.
type Pokemon struct {
	ID             int
	Name           string
	Classification string
	Types          []string
	Resistant      []string
	Weaknesses     []string
	Weight         Range // kg
	Height         Range // m
	FleeRate       float64
	MaxCP          int
	MaxHP          int
	Class          string // "", ClassLegendary or ClassMythic
	CaptureArea    string // optional hint about where it is commonly found
	Attacks        Attacks

	EvolutionRequirement *EvolutionRequirement
	Evolutions           []EvolutionRef // pokemons this one evolves into
	PreviousEvolutions   []EvolutionRef // pokemons this one evolves from

	IsFavorite bool
}

// SortField is a field a pokemon list can be ordered by.
type SortField string

// Supported sort fields.
const (
	SortByID    SortField = "id"
	SortByName  SortField = "name"
	SortByMaxCP SortField = "maxCP"
	SortByMaxHP SortField = "maxHP"
)

// SortOrder is the direction of a sort.
type SortOrder string

// Supported sort directions.
const (
	OrderAsc  SortOrder = "asc"
	OrderDesc SortOrder = "desc"
)

// ListQuery describes a filtered, sorted and paginated listing of pokemons.
// Zero values mean "no filter" / "use default".
type ListQuery struct {
	Name      string   // case-insensitive substring match
	Types     []string // pokemon must have ALL of these types
	Weakness  string   // pokemon must be weak against this type
	Resistant string   // pokemon must resist this type
	Class     string   // ClassLegendary or ClassMythic
	Favorite  *bool    // nil: any; true: only favorites; false: only non-favorites

	Sort  SortField
	Order SortOrder

	Page     int
	PageSize int
}

// Page is one page of a pokemon listing.
type Page struct {
	Items    []Pokemon
	Total    int // total number of matches across all pages
	Page     int
	PageSize int
}

// CanonicalType normalizes a type name to the data set's casing ("fIRE" -> "Fire").
func CanonicalType(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
