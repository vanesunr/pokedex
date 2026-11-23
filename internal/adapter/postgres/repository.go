// repository.go implements app.Repository with SQL queries against
// PostgreSQL: lookups, filtered/sorted/paginated listing and favorites.

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"pokedex/internal/app"
	"pokedex/internal/domain"
)

// Repository implements app.Repository on top of PostgreSQL.
type Repository struct {
	pool *pgxpool.Pool
}

var _ app.Repository = (*Repository)(nil)

// NewRepository builds a Repository using an existing connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetByID implements app.Repository.
func (r *Repository) GetByID(ctx context.Context, id int) (domain.Pokemon, error) {
	return r.getOne(ctx, `p.id = $1`, id)
}

// GetByName implements app.Repository.
func (r *Repository) GetByName(ctx context.Context, name string) (domain.Pokemon, error) {
	return r.getOne(ctx, `lower(p.name) = lower($1)`, name)
}

// getOne loads a single pokemon matching the given WHERE condition (with one
// bound argument) together with its relations, or returns domain.ErrNotFound.
func (r *Repository) getOne(ctx context.Context, where string, arg any) (domain.Pokemon, error) {
	rows, err := r.pool.Query(ctx, baseSelect+` WHERE `+where, arg)
	if err != nil {
		return domain.Pokemon{}, err
	}
	pokemons, err := scanBase(rows)
	if err != nil {
		return domain.Pokemon{}, err
	}
	if len(pokemons) == 0 {
		return domain.Pokemon{}, domain.ErrNotFound
	}
	if err := r.loadRelations(ctx, pokemons); err != nil {
		return domain.Pokemon{}, err
	}
	return pokemons[0], nil
}

// List implements app.Repository.
func (r *Repository) List(ctx context.Context, q domain.ListQuery) (domain.Page, error) {
	where, args := buildWhere(q)

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM pokemons p `+where, args...).Scan(&total); err != nil {
		return domain.Page{}, err
	}

	// The id is the tie-breaker, which makes pagination deterministic.
	orderBy := fmt.Sprintf(` ORDER BY %s %s, p.id ASC`, sortColumn(q.Sort), sortDirection(q.Order))
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	query := fmt.Sprintf(`%s %s %s LIMIT $%d OFFSET $%d`,
		baseSelect, where, orderBy, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.Page{}, err
	}
	pokemons, err := scanBase(rows)
	if err != nil {
		return domain.Page{}, err
	}
	if err := r.loadRelations(ctx, pokemons); err != nil {
		return domain.Page{}, err
	}
	return domain.Page{Items: pokemons, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// Types implements app.Repository.
func (r *Repository) Types(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT name FROM types ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AddFavorite implements app.Repository.
func (r *Repository) AddFavorite(ctx context.Context, id int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO favorites (pokemon_id) VALUES ($1) ON CONFLICT DO NOTHING`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return domain.ErrNotFound
	}
	return err
}

// RemoveFavorite implements app.Repository.
func (r *Repository) RemoveFavorite(ctx context.Context, id int) error {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pokemons WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM favorites WHERE pokemon_id = $1`, id)
	return err
}

// baseSelect selects the scalar columns of a pokemon plus whether it is a
// favorite. The list-valued relations are loaded separately by loadRelations.
const baseSelect = `SELECT p.id, p.name, p.classification, p.weight_min_kg, p.weight_max_kg,
	p.height_min_m, p.height_max_m, p.flee_rate, p.max_cp, p.max_hp,
	p.class, p.capture_area, p.evolution_candies, p.evolution_candy_name,
	EXISTS (SELECT 1 FROM favorites f WHERE f.pokemon_id = p.id) AS is_favorite
	FROM pokemons p`

// buildWhere translates the filters of q into a WHERE clause (including the
// keyword) and its positional arguments. All values are bound parameters.
func buildWhere(q domain.ListQuery) (string, []any) {
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	hasRelation := func(table, typ string) string {
		return fmt.Sprintf(`EXISTS (SELECT 1 FROM %s x WHERE x.pokemon_id = p.id AND x.type = %s)`, table, arg(typ))
	}

	if q.Name != "" {
		conds = append(conds, `p.name ILIKE `+arg("%"+escapeLike(q.Name)+"%"))
	}
	for _, t := range q.Types {
		conds = append(conds, hasRelation("pokemon_types", t))
	}
	if q.Weakness != "" {
		conds = append(conds, hasRelation("pokemon_weaknesses", q.Weakness))
	}
	if q.Resistant != "" {
		conds = append(conds, hasRelation("pokemon_resistances", q.Resistant))
	}
	if q.Class != "" {
		conds = append(conds, `p.class = `+arg(q.Class))
	}
	if q.Favorite != nil {
		cond := `EXISTS (SELECT 1 FROM favorites f WHERE f.pokemon_id = p.id)`
		if !*q.Favorite {
			cond = `NOT ` + cond
		}
		conds = append(conds, cond)
	}

	if len(conds) == 0 {
		return "", args
	}
	return `WHERE ` + strings.Join(conds, ` AND `), args
}

// escapeLike escapes the LIKE wildcards (% and _) and the escape character
// itself so that user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// sortColumn maps a validated sort field to its column. Never interpolate
// user input here: only the whitelisted values below reach the SQL text.
func sortColumn(f domain.SortField) string {
	switch f {
	case domain.SortByName:
		return `lower(p.name)`
	case domain.SortByMaxCP:
		return `p.max_cp`
	case domain.SortByMaxHP:
		return `p.max_hp`
	default:
		return `p.id`
	}
}

// sortDirection maps a sort order to the SQL keyword, defaulting to ASC.
func sortDirection(o domain.SortOrder) string {
	if o == domain.OrderDesc {
		return `DESC`
	}
	return `ASC`
}

// scanBase reads the rows produced by baseSelect into pokemons. Nullable
// columns are converted to optional fields and the list fields are
// initialized to empty slices, to be filled in by loadRelations.
func scanBase(rows pgx.Rows) ([]domain.Pokemon, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Pokemon, error) {
		var (
			p                  domain.Pokemon
			class, area, candy *string
			candies            *int
		)
		err := row.Scan(&p.ID, &p.Name, &p.Classification, &p.Weight.Min, &p.Weight.Max,
			&p.Height.Min, &p.Height.Max, &p.FleeRate, &p.MaxCP, &p.MaxHP,
			&class, &area, &candies, &candy, &p.IsFavorite)
		if err != nil {
			return p, err
		}
		if class != nil {
			p.Class = *class
		}
		if area != nil {
			p.CaptureArea = *area
		}
		if candies != nil && candy != nil {
			p.EvolutionRequirement = &domain.EvolutionRequirement{Amount: *candies, CandyName: *candy}
		}
		p.Types, p.Resistant, p.Weaknesses = []string{}, []string{}, []string{}
		p.Evolutions, p.PreviousEvolutions = []domain.EvolutionRef{}, []domain.EvolutionRef{}
		p.Attacks = domain.Attacks{Fast: []domain.Attack{}, Special: []domain.Attack{}}
		return p, nil
	})
}

// loadRelations fills the types, weaknesses, resistances, attacks and
// evolutions of the given pokemons with one query per relation (not per
// pokemon), avoiding the N+1 problem.
func (r *Repository) loadRelations(ctx context.Context, pokemons []domain.Pokemon) error {
	if len(pokemons) == 0 {
		return nil
	}
	ids := make([]int, len(pokemons))
	index := make(map[int]*domain.Pokemon, len(pokemons))
	for i := range pokemons {
		ids[i] = pokemons[i].ID
		index[pokemons[i].ID] = &pokemons[i]
	}

	typeLists := []struct {
		table string
		order string
		dst   func(*domain.Pokemon) *[]string
	}{
		{"pokemon_types", "slot", func(p *domain.Pokemon) *[]string { return &p.Types }},
		{"pokemon_weaknesses", "type", func(p *domain.Pokemon) *[]string { return &p.Weaknesses }},
		{"pokemon_resistances", "type", func(p *domain.Pokemon) *[]string { return &p.Resistant }},
	}
	for _, tl := range typeLists {
		rows, err := r.pool.Query(ctx, fmt.Sprintf(
			`SELECT pokemon_id, type FROM %s WHERE pokemon_id = ANY($1) ORDER BY pokemon_id, %s`, tl.table, tl.order), ids)
		if err != nil {
			return err
		}
		if err := forEachRow(rows, func(id int, typ string) {
			dst := tl.dst(index[id])
			*dst = append(*dst, typ)
		}); err != nil {
			return err
		}
	}

	attackRows, err := r.pool.Query(ctx, `SELECT pa.pokemon_id, a.name, a.category, a.type, a.damage
		FROM pokemon_attacks pa JOIN attacks a ON a.id = pa.attack_id
		WHERE pa.pokemon_id = ANY($1) ORDER BY pa.pokemon_id, a.name`, ids)
	if err != nil {
		return err
	}
	defer attackRows.Close()
	for attackRows.Next() {
		var (
			id       int
			category string
			a        domain.Attack
		)
		if err := attackRows.Scan(&id, &a.Name, &category, &a.Type, &a.Damage); err != nil {
			return err
		}
		p := index[id]
		if category == "fast" {
			p.Attacks.Fast = append(p.Attacks.Fast, a)
		} else {
			p.Attacks.Special = append(p.Attacks.Special, a)
		}
	}
	if err := attackRows.Err(); err != nil {
		return err
	}

	// Next evolutions, then previous evolutions.
	evolutionQueries := []struct {
		query string
		dst   func(*domain.Pokemon) *[]domain.EvolutionRef
	}{
		{`SELECT e.pokemon_id, t.id, t.name FROM pokemon_evolutions e JOIN pokemons t ON t.id = e.evolves_to_id
			WHERE e.pokemon_id = ANY($1) ORDER BY e.pokemon_id, t.id`,
			func(p *domain.Pokemon) *[]domain.EvolutionRef { return &p.Evolutions }},
		{`SELECT e.evolves_to_id, f.id, f.name FROM pokemon_evolutions e JOIN pokemons f ON f.id = e.pokemon_id
			WHERE e.evolves_to_id = ANY($1) ORDER BY e.evolves_to_id, f.id`,
			func(p *domain.Pokemon) *[]domain.EvolutionRef { return &p.PreviousEvolutions }},
	}
	for _, eq := range evolutionQueries {
		rows, err := r.pool.Query(ctx, eq.query, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id  int
				ref domain.EvolutionRef
			)
			if err := rows.Scan(&id, &ref.ID, &ref.Name); err != nil {
				return err
			}
			dst := eq.dst(index[id])
			*dst = append(*dst, ref)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// forEachRow iterates over a two column (int, string) result set and closes it.
func forEachRow(rows pgx.Rows, fn func(int, string)) error {
	defer rows.Close()
	for rows.Next() {
		var (
			id int
			s  string
		)
		if err := rows.Scan(&id, &s); err != nil {
			return err
		}
		fn(id, s)
	}
	return rows.Err()
}
