// Package httpapi is the HTTP/JSON adapter in front of the application service.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"pokedex/internal/domain"
)

// Service is what the HTTP layer needs from the application. app.Service
// satisfies it; declaring it here keeps the handlers easy to test.
type Service interface {
	GetByID(ctx context.Context, id int) (domain.Pokemon, error)
	GetByName(ctx context.Context, name string) (domain.Pokemon, error)
	List(ctx context.Context, q domain.ListQuery) (domain.Page, error)
	ListByType(ctx context.Context, typ string, q domain.ListQuery) (domain.Page, error)
	ListFavorites(ctx context.Context, q domain.ListQuery) (domain.Page, error)
	Types(ctx context.Context) ([]string, error)
	AddFavorite(ctx context.Context, id int) error
	RemoveFavorite(ctx context.Context, id int) error
}

// Handler serves the REST API.
type Handler struct {
	svc    Service
	logger *slog.Logger
}

// NewHandler returns the http.Handler with all the routes registered.
func NewHandler(svc Service, logger *slog.Logger) http.Handler {
	h := &Handler{svc: svc, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /pokemons", h.listPokemons)
	mux.HandleFunc("GET /pokemons/{id}", h.getPokemon)
	mux.HandleFunc("GET /pokemons/name/{name}", h.getPokemonByName)
	mux.HandleFunc("GET /types", h.listTypes)
	mux.HandleFunc("GET /types/{type}/pokemons", h.listPokemonsByType)
	mux.HandleFunc("GET /favorites", h.listFavorites)
	mux.HandleFunc("PUT /favorites/{id}", h.addFavorite)
	mux.HandleFunc("DELETE /favorites/{id}", h.removeFavorite)
	return h.recoverPanic(mux)
}

// getPokemon handles GET /pokemons/{id}: returns one pokemon by Pokedex number.
func (h *Handler) getPokemon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	p, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPokemonDTO(p))
}

// getPokemonByName handles GET /pokemons/name/{name}: returns one pokemon by
// its case-insensitive name.
func (h *Handler) getPokemonByName(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetByName(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPokemonDTO(p))
}

// listPokemons handles GET /pokemons: a filtered, sorted and paginated list.
func (h *Handler) listPokemons(w http.ResponseWriter, r *http.Request) {
	q, err := parseListQuery(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.svc.List(r.Context(), q)
	h.respondPage(w, r, page, err)
}

// listPokemonsByType handles GET /types/{type}/pokemons: the same list as
// listPokemons, restricted to pokemons of the type in the path.
func (h *Handler) listPokemonsByType(w http.ResponseWriter, r *http.Request) {
	q, err := parseListQuery(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.svc.ListByType(r.Context(), r.PathValue("type"), q)
	h.respondPage(w, r, page, err)
}

// listFavorites handles GET /favorites: the same list as listPokemons,
// restricted to favorite pokemons.
func (h *Handler) listFavorites(w http.ResponseWriter, r *http.Request) {
	q, err := parseListQuery(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.svc.ListFavorites(r.Context(), q)
	h.respondPage(w, r, page, err)
}

// listTypes handles GET /types: returns every pokemon type.
func (h *Handler) listTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.svc.Types(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": types})
}

// addFavorite handles PUT /favorites/{id}: marks a pokemon as favorite.
func (h *Handler) addFavorite(w http.ResponseWriter, r *http.Request) {
	h.changeFavorite(w, r, h.svc.AddFavorite)
}

// removeFavorite handles DELETE /favorites/{id}: unmarks a pokemon as favorite.
func (h *Handler) removeFavorite(w http.ResponseWriter, r *http.Request) {
	h.changeFavorite(w, r, h.svc.RemoveFavorite)
}

// changeFavorite is the shared body of addFavorite and removeFavorite: it
// parses the id from the path, calls fn and replies 204 No Content on success.
func (h *Handler) changeFavorite(w http.ResponseWriter, r *http.Request, fn func(context.Context, int) error) {
	id, err := parseID(r.PathValue("id"))
	if err == nil {
		err = fn(r.Context(), id)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// respondPage writes page as JSON with status 200, or maps err to an error
// response if the service call failed.
func (h *Handler) respondPage(w http.ResponseWriter, r *http.Request, page domain.Page, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPageDTO(page))
}

// writeError maps domain errors to HTTP statuses. Unexpected errors are
// logged and never leaked to the client.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: err.Error()})
	case errors.Is(err, domain.ErrInvalidArgument):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	default:
		h.logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal server error"})
	}
}

// recoverPanic is a middleware that turns a panic in a handler into a logged
// 500 response instead of dropping the connection.
func (h *Handler) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				h.logger.ErrorContext(r.Context(), "panic", "panic", rec)
				writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// errorBody is the JSON shape of every error response.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON sends v as a JSON response with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// parseID parses a path id, which must be a positive integer.
func parseID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: id must be a positive integer", domain.ErrInvalidArgument)
	}
	return id, nil
}

// parseListQuery reads the common list parameters:
// page, page_size, name, type (repeatable), weakness, resistant, class,
// favorite, sort and order. Semantic validation lives in the service.
func parseListQuery(r *http.Request) (domain.ListQuery, error) {
	v := r.URL.Query()
	q := domain.ListQuery{
		Name:      v.Get("name"),
		Types:     v["type"],
		Weakness:  v.Get("weakness"),
		Resistant: v.Get("resistant"),
		Class:     v.Get("class"),
		Sort:      domain.SortField(v.Get("sort")),
		Order:     domain.SortOrder(v.Get("order")),
	}
	var err error
	if q.Page, err = intParam(v.Get("page"), "page"); err != nil {
		return q, err
	}
	if q.PageSize, err = intParam(v.Get("page_size"), "page_size"); err != nil {
		return q, err
	}
	if s := v.Get("favorite"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return q, fmt.Errorf("%w: favorite must be true or false", domain.ErrInvalidArgument)
		}
		q.Favorite = &b
	}
	return q, nil
}

// intParam parses an optional integer query parameter; an empty value yields
// 0, which the service later replaces with the default. An explicit 0 is
// rejected so it cannot be mistaken for "not provided".
func intParam(s, name string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be an integer", domain.ErrInvalidArgument, name)
	}
	if n == 0 {
		return 0, fmt.Errorf("%w: %s must not be 0", domain.ErrInvalidArgument, name)
	}
	return n, nil
}
