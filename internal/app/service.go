// Package app contains the use cases of the Pokedex. It depends only on the
// domain and on the ports (interfaces) it declares itself.
package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"pokedex/internal/domain"
)

const (
	// DefaultPageSize is used when the caller does not ask for a page size.
	DefaultPageSize = 20
	// MaxPageSize bounds how many items one request can pull.
	MaxPageSize = 100
)

// Repository is the persistence port. It is declared here, next to its
// consumer, so that adapters depend on the application and not vice versa.
type Repository interface {
	// GetByID returns domain.ErrNotFound if the pokemon does not exist.
	GetByID(ctx context.Context, id int) (domain.Pokemon, error)
	// GetByName matches the name case-insensitively and returns
	// domain.ErrNotFound if there is no match.
	GetByName(ctx context.Context, name string) (domain.Pokemon, error)
	// List returns the page of pokemons matching the (already validated) query.
	List(ctx context.Context, q domain.ListQuery) (domain.Page, error)
	// Types returns every pokemon type in alphabetical order.
	Types(ctx context.Context) ([]string, error)
	// AddFavorite is idempotent and returns domain.ErrNotFound if the pokemon
	// does not exist.
	AddFavorite(ctx context.Context, id int) error
	// RemoveFavorite is idempotent and returns domain.ErrNotFound if the
	// pokemon does not exist.
	RemoveFavorite(ctx context.Context, id int) error
}

// Service implements the Pokedex use cases.
type Service struct {
	repo Repository
}

// NewService builds a Service on top of a Repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// GetByID returns one pokemon by its Pokedex number.
func (s *Service) GetByID(ctx context.Context, id int) (domain.Pokemon, error) {
	if id < 1 {
		return domain.Pokemon{}, invalid("id must be a positive integer")
	}
	return s.repo.GetByID(ctx, id)
}

// GetByName returns one pokemon by its (case-insensitive) name.
func (s *Service) GetByName(ctx context.Context, name string) (domain.Pokemon, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Pokemon{}, invalid("name must not be empty")
	}
	return s.repo.GetByName(ctx, name)
}

// List returns a filtered, sorted and paginated list of pokemons.
func (s *Service) List(ctx context.Context, q domain.ListQuery) (domain.Page, error) {
	q, err := normalize(q)
	if err != nil {
		return domain.Page{}, err
	}
	return s.repo.List(ctx, q)
}

// ListByType returns the pokemons that have the given type, using the paging
// and sorting settings of q (any other filter in q is kept). An unknown type
// returns domain.ErrNotFound.
func (s *Service) ListByType(ctx context.Context, typ string, q domain.ListQuery) (domain.Page, error) {
	typ = domain.CanonicalType(typ)
	known, err := s.repo.Types(ctx)
	if err != nil {
		return domain.Page{}, err
	}
	if !slices.Contains(known, typ) {
		return domain.Page{}, fmt.Errorf("unknown type %q: %w", typ, domain.ErrNotFound)
	}
	q.Types = append(append([]string(nil), q.Types...), typ)
	return s.List(ctx, q)
}

// ListFavorites returns the favorite pokemons, with the same filters and
// paging as List.
func (s *Service) ListFavorites(ctx context.Context, q domain.ListQuery) (domain.Page, error) {
	fav := true
	q.Favorite = &fav
	return s.List(ctx, q)
}

// Types returns all the pokemon types.
func (s *Service) Types(ctx context.Context) ([]string, error) {
	return s.repo.Types(ctx)
}

// AddFavorite marks a pokemon as favorite. Doing it twice is not an error.
func (s *Service) AddFavorite(ctx context.Context, id int) error {
	if id < 1 {
		return invalid("id must be a positive integer")
	}
	return s.repo.AddFavorite(ctx, id)
}

// RemoveFavorite unmarks a pokemon as favorite. Doing it twice is not an error.
func (s *Service) RemoveFavorite(ctx context.Context, id int) error {
	if id < 1 {
		return invalid("id must be a positive integer")
	}
	return s.repo.RemoveFavorite(ctx, id)
}

// normalize validates a query and fills in the defaults.
func normalize(q domain.ListQuery) (domain.ListQuery, error) {
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = DefaultPageSize
	}
	if q.Page < 1 {
		return q, invalid("page must be >= 1")
	}
	if q.PageSize < 1 || q.PageSize > MaxPageSize {
		return q, invalid("page_size must be between 1 and %d", MaxPageSize)
	}

	switch q.Sort {
	case "":
		q.Sort = domain.SortByID
	case domain.SortByID, domain.SortByName, domain.SortByMaxCP, domain.SortByMaxHP:
	default:
		return q, invalid("unsupported sort field %q", q.Sort)
	}

	switch q.Order {
	case "":
		q.Order = domain.OrderAsc
	case domain.OrderAsc, domain.OrderDesc:
	default:
		return q, invalid("order must be %q or %q", domain.OrderAsc, domain.OrderDesc)
	}

	switch strings.ToUpper(q.Class) {
	case "":
		q.Class = ""
	case domain.ClassLegendary, domain.ClassMythic:
		q.Class = strings.ToUpper(q.Class)
	default:
		return q, invalid("class must be %q or %q", domain.ClassLegendary, domain.ClassMythic)
	}

	q.Name = strings.TrimSpace(q.Name)
	q.Weakness = domain.CanonicalType(q.Weakness)
	q.Resistant = domain.CanonicalType(q.Resistant)
	types := make([]string, 0, len(q.Types))
	for _, t := range q.Types {
		if t = domain.CanonicalType(t); t != "" {
			types = append(types, t)
		}
	}
	q.Types = types
	return q, nil
}

// invalid builds an error that wraps domain.ErrInvalidArgument with a
// formatted message, so callers can detect it with errors.Is.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalidArgument, fmt.Sprintf(format, args...))
}
