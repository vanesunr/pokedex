// service_test.go tests the application service against an in-memory fake
// repository, so no database is needed.

package app

import (
	"context"
	"errors"
	"testing"

	"pokedex/internal/domain"
)

// fakeRepo records what the service passes down.
type fakeRepo struct {
	listQuery domain.ListQuery
	pokemon   domain.Pokemon
	err       error
	favAdded  int
	favRemove int
}

// GetByID returns the canned pokemon and error.
func (f *fakeRepo) GetByID(context.Context, int) (domain.Pokemon, error) { return f.pokemon, f.err }

// GetByName returns the canned pokemon and error.
func (f *fakeRepo) GetByName(context.Context, string) (domain.Pokemon, error) {
	return f.pokemon, f.err
}

// List records the query it received and returns an empty page.
func (f *fakeRepo) List(_ context.Context, q domain.ListQuery) (domain.Page, error) {
	f.listQuery = q
	return domain.Page{Page: q.Page, PageSize: q.PageSize}, f.err
}

// Types returns a single fixed type.
func (f *fakeRepo) Types(context.Context) ([]string, error) { return []string{"Fire"}, f.err }

// AddFavorite records the id it was called with.
func (f *fakeRepo) AddFavorite(_ context.Context, id int) error {
	f.favAdded = id
	return f.err
}

// RemoveFavorite records the id it was called with.
func (f *fakeRepo) RemoveFavorite(_ context.Context, id int) error {
	f.favRemove = id
	return f.err
}

// TestService_List_DefaultsAndNormalization checks that List fills in the
// default paging/sorting and cleans up names, types and class.
func TestService_List_DefaultsAndNormalization(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	_, err := svc.List(context.Background(), domain.ListQuery{
		Name: "  char ", Types: []string{"fIRE", " "}, Weakness: "water", Class: "legendary",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := repo.listQuery
	if got.Page != 1 || got.PageSize != DefaultPageSize {
		t.Errorf("paging defaults: %d/%d", got.Page, got.PageSize)
	}
	if got.Sort != domain.SortByID || got.Order != domain.OrderAsc {
		t.Errorf("sort defaults: %s/%s", got.Sort, got.Order)
	}
	if got.Name != "char" || len(got.Types) != 1 || got.Types[0] != "Fire" ||
		got.Weakness != "Water" || got.Class != domain.ClassLegendary {
		t.Errorf("normalization failed: %+v", got)
	}
}

// TestService_List_Validation checks that invalid queries are rejected with
// ErrInvalidArgument.
func TestService_List_Validation(t *testing.T) {
	tests := map[string]domain.ListQuery{
		"negative page":     {Page: -1},
		"page size zero+":   {PageSize: -5},
		"page size too big": {PageSize: MaxPageSize + 1},
		"bad sort":          {Sort: "weight"},
		"bad order":         {Order: "up"},
		"bad class":         {Class: "rare"},
	}
	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			_, err := NewService(repo).List(context.Background(), q)
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("err = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// TestService_ListByType_AddsTypeFilter checks that the type is appended to
// the existing filters without mutating the caller's slice.
func TestService_ListByType_AddsTypeFilter(t *testing.T) {
	repo := &fakeRepo{}
	in := domain.ListQuery{Types: []string{"flying"}}
	if _, err := NewService(repo).ListByType(context.Background(), "fire", in); err != nil {
		t.Fatal(err)
	}
	if got := repo.listQuery.Types; len(got) != 2 || got[0] != "Flying" || got[1] != "Fire" {
		t.Errorf("types = %v", got)
	}
	if len(in.Types) != 1 {
		t.Errorf("caller slice was mutated: %v", in.Types)
	}
}

// TestService_ListByType_UnknownType checks that a type that does not exist
// returns ErrNotFound instead of an empty list.
func TestService_ListByType_UnknownType(t *testing.T) {
	_, err := NewService(&fakeRepo{}).ListByType(context.Background(), "nope", domain.ListQuery{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestService_ListFavorites_FiltersFavorites checks that ListFavorites sets
// the favorite filter to true.
func TestService_ListFavorites_FiltersFavorites(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := NewService(repo).ListFavorites(context.Background(), domain.ListQuery{}); err != nil {
		t.Fatal(err)
	}
	if f := repo.listQuery.Favorite; f == nil || !*f {
		t.Errorf("favorite filter = %v, want true", f)
	}
}

// TestService_IDValidation checks that non-positive ids and blank names are
// rejected before reaching the repository.
func TestService_IDValidation(t *testing.T) {
	svc := NewService(&fakeRepo{})
	ctx := context.Background()
	for _, id := range []int{0, -3} {
		if _, err := svc.GetByID(ctx, id); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("GetByID(%d) err = %v", id, err)
		}
		if err := svc.AddFavorite(ctx, id); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("AddFavorite(%d) err = %v", id, err)
		}
		if err := svc.RemoveFavorite(ctx, id); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("RemoveFavorite(%d) err = %v", id, err)
		}
	}
	if _, err := svc.GetByName(ctx, "  "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("GetByName(blank) err = %v", err)
	}
}

// TestService_DelegatesToRepository checks that valid calls reach the
// repository and that its errors are propagated.
func TestService_DelegatesToRepository(t *testing.T) {
	repo := &fakeRepo{pokemon: domain.Pokemon{ID: 25, Name: "Pikachu"}}
	svc := NewService(repo)
	ctx := context.Background()

	if p, err := svc.GetByID(ctx, 25); err != nil || p.Name != "Pikachu" {
		t.Errorf("GetByID = %+v, %v", p, err)
	}
	if p, err := svc.GetByName(ctx, "pikachu"); err != nil || p.ID != 25 {
		t.Errorf("GetByName = %+v, %v", p, err)
	}
	if err := svc.AddFavorite(ctx, 25); err != nil || repo.favAdded != 25 {
		t.Errorf("AddFavorite: %v / %d", err, repo.favAdded)
	}
	if err := svc.RemoveFavorite(ctx, 25); err != nil || repo.favRemove != 25 {
		t.Errorf("RemoveFavorite: %v / %d", err, repo.favRemove)
	}
	if types, err := svc.Types(ctx); err != nil || len(types) != 1 {
		t.Errorf("Types = %v, %v", types, err)
	}

	repo.err = domain.ErrNotFound
	if _, err := svc.GetByID(ctx, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("not found not propagated: %v", err)
	}
}
