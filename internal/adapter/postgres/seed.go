// seed.go loads the parsed pokemon data set into PostgreSQL.

package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pokedex/internal/domain"
)

// Seed loads pokemons into the database inside a single transaction. It is
// idempotent: running it again updates the catalog in place and keeps the
// favorites.
//
// The source data has a few inconsistencies, which are handled here:
//   - evolution links are given from both sides ("evolutions" and "Previous
//     evolution(s)") and do not always agree (e.g. Dratini does not list
//     Dragonite although Dragonite lists Dratini), so the union of both is stored;
//   - Jigglypuff and Wigglytuff reference themselves; self links are dropped.
func Seed(ctx context.Context, pool *pgxpool.Pool, pokemons []domain.Pokemon) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	b := &pgx.Batch{}

	for _, t := range collectTypes(pokemons) {
		b.Queue(`INSERT INTO types (name) VALUES ($1) ON CONFLICT DO NOTHING`, t)
	}
	for _, p := range pokemons {
		queuePokemon(b, p)
	}
	// Evolutions reference other pokemons, so they go after all the pokemons.
	for _, e := range evolutionEdges(pokemons) {
		b.Queue(`INSERT INTO pokemon_evolutions (pokemon_id, evolves_to_id) VALUES ($1, $2)`, e[0], e[1])
	}

	res := tx.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := res.Exec(); err != nil {
			res.Close()
			return fmt.Errorf("seed statement %d: %w", i, err)
		}
	}
	if err := res.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// queuePokemon adds to the batch the statements that upsert one pokemon row,
// replace its child rows (types, weaknesses, resistances, attacks) and
// delete its outgoing evolution links, which are re-inserted afterwards.
func queuePokemon(b *pgx.Batch, p domain.Pokemon) {
	var candies *int
	var candyName *string
	if r := p.EvolutionRequirement; r != nil {
		candies, candyName = &r.Amount, &r.CandyName
	}
	b.Queue(`INSERT INTO pokemons (id, name, classification, weight_min_kg, weight_max_kg,
			height_min_m, height_max_m, flee_rate, max_cp, max_hp, class, capture_area,
			evolution_candies, evolution_candy_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, classification=EXCLUDED.classification,
			weight_min_kg=EXCLUDED.weight_min_kg, weight_max_kg=EXCLUDED.weight_max_kg,
			height_min_m=EXCLUDED.height_min_m, height_max_m=EXCLUDED.height_max_m,
			flee_rate=EXCLUDED.flee_rate, max_cp=EXCLUDED.max_cp, max_hp=EXCLUDED.max_hp,
			class=EXCLUDED.class, capture_area=EXCLUDED.capture_area,
			evolution_candies=EXCLUDED.evolution_candies,
			evolution_candy_name=EXCLUDED.evolution_candy_name`,
		p.ID, p.Name, p.Classification, p.Weight.Min, p.Weight.Max, p.Height.Min, p.Height.Max,
		p.FleeRate, p.MaxCP, p.MaxHP, nullIfEmpty(p.Class), nullIfEmpty(p.CaptureArea), candies, candyName)

	// Replace the child rows so that a re-seed reflects the file exactly.
	for _, table := range []string{"pokemon_types", "pokemon_weaknesses", "pokemon_resistances", "pokemon_attacks"} {
		b.Queue(fmt.Sprintf(`DELETE FROM %s WHERE pokemon_id = $1`, table), p.ID)
	}
	b.Queue(`DELETE FROM pokemon_evolutions WHERE pokemon_id = $1`, p.ID)

	for i, t := range p.Types {
		b.Queue(`INSERT INTO pokemon_types (pokemon_id, type, slot) VALUES ($1,$2,$3)`, p.ID, t, i)
	}
	for _, t := range p.Weaknesses {
		b.Queue(`INSERT INTO pokemon_weaknesses (pokemon_id, type) VALUES ($1,$2)`, p.ID, t)
	}
	for _, t := range p.Resistant {
		b.Queue(`INSERT INTO pokemon_resistances (pokemon_id, type) VALUES ($1,$2)`, p.ID, t)
	}
	queueAttacks(b, p.ID, "fast", p.Attacks.Fast)
	queueAttacks(b, p.ID, "special", p.Attacks.Special)
}

// queueAttacks adds to the batch the statements that upsert each attack
// (attacks are shared between pokemons) and link it to the given pokemon.
func queueAttacks(b *pgx.Batch, pokemonID int, category string, attacks []domain.Attack) {
	for _, a := range attacks {
		b.Queue(`INSERT INTO attacks (name, category, type, damage) VALUES ($1,$2,$3,$4)
			ON CONFLICT (name, category) DO UPDATE SET type=EXCLUDED.type, damage=EXCLUDED.damage`,
			a.Name, category, a.Type, a.Damage)
		b.Queue(`INSERT INTO pokemon_attacks (pokemon_id, attack_id)
			SELECT $1, id FROM attacks WHERE name=$2 AND category=$3`, pokemonID, a.Name, category)
	}
}

// collectTypes returns every distinct type name referenced by the data.
func collectTypes(pokemons []domain.Pokemon) []string {
	set := map[string]struct{}{}
	for _, p := range pokemons {
		for _, group := range [][]string{p.Types, p.Weaknesses, p.Resistant} {
			for _, t := range group {
				set[t] = struct{}{}
			}
		}
		for _, list := range [][]domain.Attack{p.Attacks.Fast, p.Attacks.Special} {
			for _, a := range list {
				set[a.Type] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// evolutionEdges returns the distinct [from, to] pairs, merging both
// directions in the source and dropping self references.
func evolutionEdges(pokemons []domain.Pokemon) [][2]int {
	seen := map[[2]int]struct{}{}
	var out [][2]int
	add := func(from, to int) {
		e := [2]int{from, to}
		if from == to {
			return
		}
		if _, ok := seen[e]; !ok {
			seen[e] = struct{}{}
			out = append(out, e)
		}
	}
	for _, p := range pokemons {
		for _, e := range p.Evolutions {
			add(p.ID, e.ID)
		}
		for _, e := range p.PreviousEvolutions {
			add(e.ID, p.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

// nullIfEmpty returns nil for an empty string so it is stored as SQL NULL.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
