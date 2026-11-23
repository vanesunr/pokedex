// postgres_test.go tests the PostgreSQL adapter. The pure helpers run always;
// the repository and seed tests need a real database (see newTestRepo).

package postgres

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"pokedex/internal/adapter/jsonfile"
	"pokedex/internal/domain"
)

// TestEvolutionEdges_MergesBothDirectionsAndDropsSelfLinks checks that links
// declared from either side are merged without duplicates and that a pokemon
// pointing to itself is ignored.
func TestEvolutionEdges_MergesBothDirectionsAndDropsSelfLinks(t *testing.T) {
	pokemons := []domain.Pokemon{
		{ID: 39, Evolutions: []domain.EvolutionRef{{ID: 39}}}, // self link in the source data
		{ID: 147, Evolutions: []domain.EvolutionRef{{ID: 148}}},
		{ID: 148, Evolutions: []domain.EvolutionRef{{ID: 149}}, PreviousEvolutions: []domain.EvolutionRef{{ID: 147}}},
		{ID: 149, PreviousEvolutions: []domain.EvolutionRef{{ID: 147}, {ID: 148}}},
	}
	want := [][2]int{{147, 148}, {147, 149}, {148, 149}}
	if got := evolutionEdges(pokemons); !reflect.DeepEqual(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
}

// TestEscapeLike checks that LIKE wildcards and backslashes are escaped.
func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_\`); got != `50\%\_\\` {
		t.Errorf("escapeLike = %q", got)
	}
}

// pokemonsFile is the path to the real data set, relative to this package.
const pokemonsFile = "../../../pokemons.json"

// loadPokemons parses the real data set, failing the test on any error.
func loadPokemons(t *testing.T) []domain.Pokemon {
	t.Helper()
	f, err := os.Open(pokemonsFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pokemons, err := jsonfile.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return pokemons
}

// newTestRepo needs a real PostgreSQL: set TEST_DATABASE_URL to run it, e.g.
//
//	TEST_DATABASE_URL=postgres://pokedex:pokedex@localhost:5432/pokedex?sslmode=disable
//
// Every test works in its own schema, so it never touches the application data.
func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower("test_" + strings.ReplaceAll(t.Name(), "/", "_"))
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Seed(ctx, pool, loadPokemons(t)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return NewRepository(pool)
}

// names returns the names of the given pokemons, in order.
func names(ps []domain.Pokemon) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// TestRepository_GetByIDAndName checks lookups by id and by name (including
// relations, data fix-ups and the not-found case).
func TestRepository_GetByIDAndName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.GetByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Bulbasaur" || !reflect.DeepEqual(p.Types, []string{"Grass", "Poison"}) {
		t.Errorf("unexpected pokemon: %+v", p)
	}
	if len(p.Attacks.Fast) != 2 || len(p.Attacks.Special) != 3 {
		t.Errorf("attacks: %+v", p.Attacks)
	}
	if len(p.Evolutions) != 2 || p.Evolutions[0].Name != "Ivysaur" || len(p.PreviousEvolutions) != 0 {
		t.Errorf("evolutions: %+v / %+v", p.Evolutions, p.PreviousEvolutions)
	}
	if p.EvolutionRequirement == nil || p.EvolutionRequirement.Amount != 25 {
		t.Errorf("evolution requirement: %+v", p.EvolutionRequirement)
	}

	byName, err := repo.GetByName(ctx, "bULBASAUR")
	if err != nil || byName.ID != 1 {
		t.Errorf("GetByName: %+v, %v", byName, err)
	}

	// Data fix-up: Dragonite's previous evolutions include Dratini and Dragonair.
	dragonite, err := repo.GetByID(ctx, 149)
	if err != nil || len(dragonite.PreviousEvolutions) != 2 {
		t.Errorf("Dragonite: %+v, %v", dragonite.PreviousEvolutions, err)
	}
	mew, _ := repo.GetByID(ctx, 151)
	if mew.Class != domain.ClassMythic {
		t.Errorf("Mew class = %q", mew.Class)
	}

	if _, err := repo.GetByID(ctx, 9999); err != domain.ErrNotFound {
		t.Errorf("GetByID(9999) err = %v", err)
	}
	if _, err := repo.GetByName(ctx, "missingno"); err != domain.ErrNotFound {
		t.Errorf("GetByName(missingno) err = %v", err)
	}
}

// TestRepository_List_PaginationAndSorting checks page boundaries, totals and
// the supported sort fields and directions.
func TestRepository_List_PaginationAndSorting(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	page, err := repo.List(ctx, domain.ListQuery{Sort: domain.SortByID, Order: domain.OrderAsc, Page: 2, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 151 || len(page.Items) != 10 || page.Items[0].ID != 11 {
		t.Errorf("total=%d len=%d first=%d", page.Total, len(page.Items), page.Items[0].ID)
	}

	last, _ := repo.List(ctx, domain.ListQuery{Sort: domain.SortByID, Order: domain.OrderAsc, Page: 4, PageSize: 50})
	if len(last.Items) != 1 || last.Items[0].ID != 151 {
		t.Errorf("last page: %v", names(last.Items))
	}

	top, _ := repo.List(ctx, domain.ListQuery{Sort: domain.SortByMaxCP, Order: domain.OrderDesc, Page: 1, PageSize: 1})
	if top.Items[0].Name != "Mewtwo" {
		t.Errorf("highest CP = %s", top.Items[0].Name)
	}
	byName, _ := repo.List(ctx, domain.ListQuery{Sort: domain.SortByName, Order: domain.OrderAsc, Page: 1, PageSize: 1})
	if byName.Items[0].Name != "Abra" {
		t.Errorf("first by name = %s", byName.Items[0].Name)
	}
}

// TestRepository_List_Filters checks the name, type, weakness, resistance and
// class filters, including that LIKE wildcards in the input are literal.
func TestRepository_List_Filters(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	list := func(f func(*domain.ListQuery)) domain.Page {
		lq := domain.ListQuery{Sort: domain.SortByID, Order: domain.OrderAsc, Page: 1, PageSize: 100}
		f(&lq)
		page, err := repo.List(ctx, lq)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}

	if p := list(func(l *domain.ListQuery) { l.Name = "char" }); !reflect.DeepEqual(names(p.Items), []string{"Charmander", "Charmeleon", "Charizard"}) {
		t.Errorf("name filter: %v", names(p.Items))
	}
	if p := list(func(l *domain.ListQuery) { l.Types = []string{"Fire", "Flying"} }); !reflect.DeepEqual(names(p.Items), []string{"Charizard", "Moltres"}) {
		t.Errorf("two types: %v", names(p.Items))
	}
	if p := list(func(l *domain.ListQuery) { l.Types = []string{"Dragon"} }); p.Total != 3 {
		t.Errorf("dragon total = %d", p.Total)
	}
	if p := list(func(l *domain.ListQuery) { l.Class = domain.ClassLegendary }); p.Total != 4 {
		t.Errorf("legendary total = %d", p.Total)
	}
	if p := list(func(l *domain.ListQuery) { l.Weakness = "Ghost"; l.Types = []string{"Ghost"} }); p.Total == 0 {
		t.Errorf("ghost weak to ghost expected")
	}
	if p := list(func(l *domain.ListQuery) { l.Resistant = "Ghost" }); p.Total == 0 {
		t.Errorf("resistant filter returned nothing")
	}
	// Wildcards in the user input are literal.
	if p := list(func(l *domain.ListQuery) { l.Name = "%" }); p.Total != 0 {
		t.Errorf("'%%' matched %d pokemons", p.Total)
	}
}

// TestRepository_Types checks that all 18 types are returned alphabetically.
func TestRepository_Types(t *testing.T) {
	repo := newTestRepo(t)
	types, err := repo.Types(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 18 || types[0] != "Bug" {
		t.Errorf("types = %v", types)
	}
}

// TestRepository_Favorites checks adding and removing favorites (idempotent),
// listing them, and the not-found errors for unknown ids.
func TestRepository_Favorites(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	fav := true
	favorites := func() domain.Page {
		page, err := repo.List(ctx, domain.ListQuery{Favorite: &fav, Sort: domain.SortByID, Order: domain.OrderAsc, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		return page
	}

	for i := 0; i < 2; i++ { // twice: must be idempotent
		if err := repo.AddFavorite(ctx, 25); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AddFavorite(ctx, 4); err != nil {
		t.Fatal(err)
	}

	page := favorites()
	if !reflect.DeepEqual(names(page.Items), []string{"Charmander", "Pikachu"}) || !page.Items[0].IsFavorite {
		t.Errorf("favorites = %v", names(page.Items))
	}
	if p, _ := repo.GetByID(ctx, 25); !p.IsFavorite {
		t.Error("GetByID should report isFavorite")
	}

	for i := 0; i < 2; i++ {
		if err := repo.RemoveFavorite(ctx, 25); err != nil {
			t.Fatal(err)
		}
	}
	if page := favorites(); len(page.Items) != 1 {
		t.Errorf("after remove: %v", names(page.Items))
	}

	if err := repo.AddFavorite(ctx, 9999); err != domain.ErrNotFound {
		t.Errorf("AddFavorite(9999) err = %v", err)
	}
	if err := repo.RemoveFavorite(ctx, 9999); err != domain.ErrNotFound {
		t.Errorf("RemoveFavorite(9999) err = %v", err)
	}
}

// TestSeed_IsIdempotentAndKeepsFavorites checks that seeding twice does not
// duplicate rows and does not lose existing favorites.
func TestSeed_IsIdempotentAndKeepsFavorites(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.AddFavorite(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, repo.pool, loadPokemons(t)); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	page, _ := repo.List(ctx, domain.ListQuery{Sort: domain.SortByID, Order: domain.OrderAsc, Page: 1, PageSize: 1})
	if page.Total != 151 {
		t.Errorf("total after reseed = %d", page.Total)
	}
	if p, _ := repo.GetByID(ctx, 1); !p.IsFavorite {
		t.Error("favorite lost after reseed")
	}
}
