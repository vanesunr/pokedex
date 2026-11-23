// handler_test.go tests the HTTP handlers with a stub service and
// httptest, checking routing, query parsing, JSON output and error mapping.

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokedex/internal/domain"
)

// stubService is a configurable Service that returns canned results and
// records the arguments it was called with.
type stubService struct {
	pokemon domain.Pokemon
	page    domain.Page
	err     error

	gotQuery domain.ListQuery
	gotType  string
	gotName  string
	gotID    int
	favCalls []string
}

// GetByID records the id and returns the canned pokemon and error.
func (s *stubService) GetByID(_ context.Context, id int) (domain.Pokemon, error) {
	s.gotID = id
	return s.pokemon, s.err
}

// GetByName records the name and returns the canned pokemon and error.
func (s *stubService) GetByName(_ context.Context, n string) (domain.Pokemon, error) {
	s.gotName = n
	return s.pokemon, s.err
}

// List records the query and returns the canned page and error.
func (s *stubService) List(_ context.Context, q domain.ListQuery) (domain.Page, error) {
	s.gotQuery = q
	return s.page, s.err
}

// ListByType records the type and query and returns the canned page and error.
func (s *stubService) ListByType(_ context.Context, t string, q domain.ListQuery) (domain.Page, error) {
	s.gotType, s.gotQuery = t, q
	return s.page, s.err
}

// ListFavorites records the query and returns the canned page and error.
func (s *stubService) ListFavorites(_ context.Context, q domain.ListQuery) (domain.Page, error) {
	s.gotQuery = q
	return s.page, s.err
}

// Types returns a single fixed type.
func (s *stubService) Types(context.Context) ([]string, error) { return []string{"Fire"}, s.err }

// AddFavorite records the id and the "add" call.
func (s *stubService) AddFavorite(_ context.Context, id int) error {
	s.gotID = id
	s.favCalls = append(s.favCalls, "add")
	return s.err
}

// RemoveFavorite records the id and the "remove" call.
func (s *stubService) RemoveFavorite(_ context.Context, id int) error {
	s.gotID = id
	s.favCalls = append(s.favCalls, "remove")
	return s.err
}

// do sends a request through a new Handler wrapping svc and returns the
// recorded response.
func do(t *testing.T, svc Service, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// TestGetPokemon checks GET /pokemons/{id}, including that empty lists are
// encoded as [].
func TestGetPokemon(t *testing.T) {
	svc := &stubService{pokemon: domain.Pokemon{ID: 25, Name: "Pikachu", Types: []string{"Electric"}, IsFavorite: true}}
	rec := do(t, svc, http.MethodGet, "/pokemons/25")

	if rec.Code != http.StatusOK || svc.gotID != 25 {
		t.Fatalf("code=%d id=%d", rec.Code, svc.gotID)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "Pikachu" || body["isFavorite"] != true {
		t.Errorf("body = %v", body)
	}
	// Collections must serialize as [] and never as null.
	if _, ok := body["evolutions"].([]any); !ok {
		t.Errorf("evolutions = %#v, want []", body["evolutions"])
	}
}

// TestGetPokemonByName checks GET /pokemons/name/{name}.
func TestGetPokemonByName(t *testing.T) {
	svc := &stubService{pokemon: domain.Pokemon{ID: 25, Name: "Pikachu"}}
	rec := do(t, svc, http.MethodGet, "/pokemons/name/pikachu")
	if rec.Code != http.StatusOK || svc.gotName != "pikachu" {
		t.Fatalf("code=%d name=%q", rec.Code, svc.gotName)
	}
}

// TestListPokemons_ParsesQuery checks that every query parameter reaches the
// service and that the page envelope (with totalPages) is returned.
func TestListPokemons_ParsesQuery(t *testing.T) {
	svc := &stubService{page: domain.Page{Total: 45, Page: 2, PageSize: 20}}
	rec := do(t, svc, http.MethodGet,
		"/pokemons?page=2&page_size=20&name=char&type=fire&type=flying&weakness=water&resistant=bug&class=legendary&favorite=false&sort=maxCP&order=desc")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	q := svc.gotQuery
	if q.Page != 2 || q.PageSize != 20 || q.Name != "char" || len(q.Types) != 2 ||
		q.Weakness != "water" || q.Resistant != "bug" || q.Class != "legendary" ||
		q.Favorite == nil || *q.Favorite || q.Sort != domain.SortByMaxCP || q.Order != domain.OrderDesc {
		t.Errorf("query = %+v", q)
	}
	var body pageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 45 || body.TotalPages != 3 || body.Items == nil {
		t.Errorf("page = %+v", body)
	}
}

// TestListByTypeAndFavorites checks the /types/{type}/pokemons, /favorites
// and /types routes.
func TestListByTypeAndFavorites(t *testing.T) {
	svc := &stubService{}
	if rec := do(t, svc, http.MethodGet, "/types/fire/pokemons"); rec.Code != http.StatusOK || svc.gotType != "fire" {
		t.Errorf("by type: code=%d type=%q", rec.Code, svc.gotType)
	}
	if rec := do(t, svc, http.MethodGet, "/favorites"); rec.Code != http.StatusOK {
		t.Errorf("favorites code=%d", rec.Code)
	}
	if rec := do(t, svc, http.MethodGet, "/types"); rec.Code != http.StatusOK {
		t.Errorf("types code=%d", rec.Code)
	}
}

// TestFavoriteEndpoints checks that PUT and DELETE /favorites/{id} reply 204
// and call the matching service method.
func TestFavoriteEndpoints(t *testing.T) {
	svc := &stubService{}
	if rec := do(t, svc, http.MethodPut, "/favorites/7"); rec.Code != http.StatusNoContent {
		t.Errorf("PUT code=%d", rec.Code)
	}
	if rec := do(t, svc, http.MethodDelete, "/favorites/7"); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE code=%d", rec.Code)
	}
	if len(svc.favCalls) != 2 || svc.favCalls[0] != "add" || svc.favCalls[1] != "remove" || svc.gotID != 7 {
		t.Errorf("calls=%v id=%d", svc.favCalls, svc.gotID)
	}
}

// TestErrorMapping checks the mapping of domain errors to HTTP statuses and
// that internal error details are not leaked.
func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target string
		want   int
	}{
		{"not found", domain.ErrNotFound, "/pokemons/999", http.StatusNotFound},
		{"invalid from service", domain.ErrInvalidArgument, "/pokemons?sort=x", http.StatusBadRequest},
		{"unexpected", errors.New("db down"), "/pokemons/1", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, &stubService{err: tt.err}, http.MethodGet, tt.target)
			if rec.Code != tt.want {
				t.Errorf("code=%d want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusInternalServerError && rec.Body.String() != "{\"error\":\"internal server error\"}\n" {
				t.Errorf("internal error leaked: %s", rec.Body)
			}
		})
	}
}

// TestBadInputIsRejectedBeforeReachingService checks that malformed ids and
// query parameters produce 400 responses.
func TestBadInputIsRejectedBeforeReachingService(t *testing.T) {
	for _, target := range []string{
		"/pokemons/abc", "/pokemons/0", "/pokemons?page=x", "/pokemons?page_size=y", "/pokemons?favorite=maybe",
		"/pokemons?page=0", "/pokemons?page_size=0",
	} {
		svc := &stubService{}
		if rec := do(t, svc, http.MethodGet, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code=%d", target, rec.Code)
		}
	}
	if rec := do(t, &stubService{}, http.MethodPut, "/favorites/abc"); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT bad id: code=%d", rec.Code)
	}
}

// TestHealthAndUnknownRoutes checks /healthz, an unknown path (404) and a
// wrong method (405).
func TestHealthAndUnknownRoutes(t *testing.T) {
	if rec := do(t, &stubService{}, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz code=%d", rec.Code)
	}
	if rec := do(t, &stubService{}, http.MethodGet, "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown code=%d", rec.Code)
	}
	if rec := do(t, &stubService{}, http.MethodPost, "/pokemons"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code=%d", rec.Code)
	}
}

// panicService is a stubService whose GetByID panics.
type panicService struct{ stubService }

// GetByID always panics, to exercise the recovery middleware.
func (panicService) GetByID(context.Context, int) (domain.Pokemon, error) { panic("boom") }

// TestPanicIsRecovered checks that a panicking handler yields a 500 response.
func TestPanicIsRecovered(t *testing.T) {
	if rec := do(t, &panicService{}, http.MethodGet, "/pokemons/1"); rec.Code != http.StatusInternalServerError {
		t.Errorf("code=%d", rec.Code)
	}
}
