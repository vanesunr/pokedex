// dto.go defines the JSON response shapes (DTOs) of the HTTP API and the
// functions that convert domain objects into them.

package httpapi

import "pokedex/internal/domain"

// The DTOs define the public JSON contract, independent of the domain types:
// the domain has no JSON tags, so renaming a field in the API never touches
// the core. The mapping functions also make sure empty lists are written as
// [] and not null, and compute derived fields such as totalPages.

// rangeDTO is the JSON form of domain.Range (a min/max pair).
type rangeDTO struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// attackDTO is the JSON form of a single domain.Attack.
type attackDTO struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Damage int    `json:"damage"`
}

// attacksDTO groups a pokemon's attacks into fast and special moves.
type attacksDTO struct {
	Fast    []attackDTO `json:"fast"`
	Special []attackDTO `json:"special"`
}

// evolutionRefDTO is a lightweight link (id and name) to another pokemon in
// an evolution chain.
type evolutionRefDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// evolutionRequirementDTO is the candy cost needed to evolve a pokemon.
type evolutionRequirementDTO struct {
	Amount    int    `json:"amount"`
	CandyName string `json:"candyName"`
}

// pokemonDTO is the full JSON representation of one pokemon. Optional fields
// (class, capture area, evolution requirement) are omitted when empty.
type pokemonDTO struct {
	ID                   int                      `json:"id"`
	Name                 string                   `json:"name"`
	Classification       string                   `json:"classification"`
	Types                []string                 `json:"types"`
	Resistant            []string                 `json:"resistant"`
	Weaknesses           []string                 `json:"weaknesses"`
	WeightKg             rangeDTO                 `json:"weightKg"`
	HeightM              rangeDTO                 `json:"heightM"`
	FleeRate             float64                  `json:"fleeRate"`
	MaxCP                int                      `json:"maxCP"`
	MaxHP                int                      `json:"maxHP"`
	Class                string                   `json:"class,omitempty"`
	CaptureArea          string                   `json:"captureArea,omitempty"`
	Attacks              attacksDTO               `json:"attacks"`
	EvolutionRequirement *evolutionRequirementDTO `json:"evolutionRequirement,omitempty"`
	Evolutions           []evolutionRefDTO        `json:"evolutions"`
	PreviousEvolutions   []evolutionRefDTO        `json:"previousEvolutions"`
	IsFavorite           bool                     `json:"isFavorite"`
}

// pageDTO is the JSON envelope of a paginated list of pokemons.
type pageDTO struct {
	Items      []pokemonDTO `json:"items"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PageSize   int          `json:"pageSize"`
	TotalPages int          `json:"totalPages"`
}

// toPokemonDTO converts a domain.Pokemon into its JSON form. Nil slices
// become empty ones so they are serialized as [] instead of null.
func toPokemonDTO(p domain.Pokemon) pokemonDTO {
	dto := pokemonDTO{
		ID:                 p.ID,
		Name:               p.Name,
		Classification:     p.Classification,
		Types:              nonNil(p.Types),
		Resistant:          nonNil(p.Resistant),
		Weaknesses:         nonNil(p.Weaknesses),
		WeightKg:           rangeDTO(p.Weight),
		HeightM:            rangeDTO(p.Height),
		FleeRate:           p.FleeRate,
		MaxCP:              p.MaxCP,
		MaxHP:              p.MaxHP,
		Class:              p.Class,
		CaptureArea:        p.CaptureArea,
		Attacks:            attacksDTO{Fast: toAttackDTOs(p.Attacks.Fast), Special: toAttackDTOs(p.Attacks.Special)},
		Evolutions:         toRefDTOs(p.Evolutions),
		PreviousEvolutions: toRefDTOs(p.PreviousEvolutions),
		IsFavorite:         p.IsFavorite,
	}
	if r := p.EvolutionRequirement; r != nil {
		dto.EvolutionRequirement = &evolutionRequirementDTO{Amount: r.Amount, CandyName: r.CandyName}
	}
	return dto
}

// toPageDTO converts a domain.Page into its JSON form and computes
// totalPages from the total item count and the page size.
func toPageDTO(p domain.Page) pageDTO {
	items := make([]pokemonDTO, len(p.Items))
	for i, it := range p.Items {
		items[i] = toPokemonDTO(it)
	}
	totalPages := 0
	if p.PageSize > 0 {
		totalPages = (p.Total + p.PageSize - 1) / p.PageSize
	}
	return pageDTO{Items: items, Total: p.Total, Page: p.Page, PageSize: p.PageSize, TotalPages: totalPages}
}

// toAttackDTOs converts a list of domain attacks; the result is never nil.
func toAttackDTOs(in []domain.Attack) []attackDTO {
	out := make([]attackDTO, len(in))
	for i, a := range in {
		out[i] = attackDTO(a)
	}
	return out
}

// toRefDTOs converts a list of evolution references; the result is never nil.
func toRefDTOs(in []domain.EvolutionRef) []evolutionRefDTO {
	out := make([]evolutionRefDTO, len(in))
	for i, r := range in {
		out[i] = evolutionRefDTO(r)
	}
	return out
}

// nonNil returns s, or an empty slice if s is nil, so it encodes as [].
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
