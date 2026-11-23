#!/usr/bin/env bash
# Smoke test for the running Pokedex API: one request per user story, plus error cases.
#
# Usage:  make start && ./scripts/test-api.sh
#         API_URL=http://localhost:8080 ./scripts/test-api.sh
#
# Each check compares the HTTP status and looks for expected text in the body.
# The script exits with 1 if any check fails. It adds and removes favorites 25
# and 150, so it removes them first and last.

API_URL="${API_URL:-http://localhost:${API_PORT:-8080}}"
body_file=$(mktemp)
trap 'rm -f "$body_file"' EXIT
pass=0
fail=0

# check "description" EXPECTED_STATUS METHOD PATH [expected text in body ...]
check() {
  local desc=$1 want=$2 method=$3 path=$4
  shift 4
  local got ok=1
  got=$(curl -s -o "$body_file" -w '%{http_code}' -X "$method" "$API_URL$path")
  [ "$got" = "$want" ] || ok=0
  for text in "$@"; do
    grep -qF -- "$text" "$body_file" || ok=0
  done
  if [ "$ok" = 1 ]; then
    pass=$((pass + 1))
    printf 'PASS  %-46s %s %s -> %s\n' "$desc" "$method" "$path" "$got"
  else
    fail=$((fail + 1))
    printf 'FAIL  %-46s %s %s -> %s (want %s, expected: %s)\n' "$desc" "$method" "$path" "$got" "$want" "$*"
    printf '      body: %.200s\n' "$(cat "$body_file")"
  fi
}

reset_favorites() {
  curl -s -o /dev/null -X DELETE "$API_URL/favorites/25"
  curl -s -o /dev/null -X DELETE "$API_URL/favorites/150"
}

echo "Testing $API_URL"
if ! curl -s -o /dev/null --max-time 3 "$API_URL/healthz"; then
  echo "Cannot reach $API_URL. Is the API running? Try: make start"
  exit 1
fi
reset_favorites

echo; echo "== Health"
check "health"                         200 GET "/healthz" '"status":"ok"'

echo; echo "== Story 1 and 3: one pokemon"
check "by id"                          200 GET "/pokemons/25" '"id":25,"name":"Pikachu"' '"isFavorite":false'
check "by name (case-insensitive)"     200 GET "/pokemons/name/charizard" '"id":6,"name":"Charizard"'

echo; echo "== Story 2: list and pagination"
check "default list"                   200 GET "/pokemons" '"total":151' '"page":1' '"pageSize":20' '"totalPages":8'
check "page 2, size 5"                 200 GET "/pokemons?page=2&page_size=5" '"page":2' '"pageSize":5' '"totalPages":31'

echo; echo "== Story 4: filters"
check "name contains 'char'"           200 GET "/pokemons?name=char" '"total":3'
check "type fire AND flying"           200 GET "/pokemons?type=fire&type=flying" '"total":2' '"name":"Charizard"' '"name":"Moltres"'
check "type fire, weak to water"       200 GET "/pokemons?type=fire&weakness=water" '"total":12'
check "class legendary"                200 GET "/pokemons?class=legendary" '"total":4'
check "class mythic"                   200 GET "/pokemons?class=mythic" '"total":1' '"name":"Mew"'
check "legendary, weak water, res bug" 200 GET "/pokemons?weakness=water&resistant=bug&class=legendary" '"name":"Moltres"'
check "filters with no match"          200 GET "/pokemons?type=grass&resistant=bug" '"total":0' '"items":[]'

echo; echo "== Story 4: sorting"
check "highest maxCP first"            200 GET "/pokemons?sort=maxCP&order=desc&page_size=1" '"name":"Mewtwo"'
check "name A-Z"                       200 GET "/pokemons?sort=name&order=asc&page_size=1" '"name":"Abra"'
check "highest maxHP first"            200 GET "/pokemons?sort=maxHP&order=desc&page_size=1" '"name":"Mewtwo"'

echo; echo "== Story 5: by type"
check "list of types"                  200 GET "/types" '"Dragon"' '"Water"'
check "dragon pokemons"                200 GET "/types/dragon/pokemons" '"total":3' '"name":"Dragonite"'
check "ghost pokemons by maxCP"        200 GET "/types/ghost/pokemons?sort=maxCP&order=desc&page_size=1" '"total":3' '"name":"Gengar"'

echo; echo "== Stories 6-8: favorites"
check "add favorite 25"                204 PUT    "/favorites/25"
check "add favorite 25 again (idempotent)" 204 PUT "/favorites/25"
check "add favorite 150"               204 PUT    "/favorites/150"
check "list favorites"                 200 GET    "/favorites" '"total":2' '"name":"Pikachu"' '"name":"Mewtwo"'
check "favorites filtered by type"     200 GET    "/favorites?type=electric" '"total":1' '"name":"Pikachu"'
check "favorite=true filter"           200 GET    "/pokemons?favorite=true" '"total":2'
check "isFavorite flag is set"         200 GET    "/pokemons/25" '"isFavorite":true'
check "remove favorite 25"             204 DELETE "/favorites/25"
check "remove favorite 25 again"       204 DELETE "/favorites/25"
check "remove favorite 150"            204 DELETE "/favorites/150"
check "favorites are empty"            200 GET    "/favorites" '"total":0' '"items":[]'

echo; echo "== Errors"
check "unknown id"                     404 GET "/pokemons/999" '"error":"pokemon not found"'
check "id is not a number"             400 GET "/pokemons/abc" 'id must be a positive integer'
check "id is 0"                        400 GET "/pokemons/0" 'id must be a positive integer'
check "unknown name"                   404 GET "/pokemons/name/missingno" '"error":"pokemon not found"'
check "page 0"                         400 GET "/pokemons?page=0" 'page must not be 0'
check "page_size over 100"             400 GET "/pokemons?page_size=101" 'page_size must be between 1 and 100'
check "bad sort field"                 400 GET "/pokemons?sort=weight" 'unsupported sort field'
check "bad order"                      400 GET "/pokemons?order=up" 'order must be'
check "bad class"                      400 GET "/pokemons?class=rare" 'class must be'
check "bad favorite value"             400 GET "/pokemons?favorite=maybe" 'favorite must be true or false'
check "unknown type"                   404 GET "/types/nope/pokemons"
check "favorite unknown pokemon (PUT)" 404 PUT "/favorites/999" '"error":"pokemon not found"'
check "favorite bad id (PUT)"          400 PUT "/favorites/abc"
check "unknown route"                  404 GET "/nope"
check "wrong method"                   405 POST "/pokemons"

reset_favorites

echo
echo "Result: $pass passed, $fail failed"
[ "$fail" = 0 ]
