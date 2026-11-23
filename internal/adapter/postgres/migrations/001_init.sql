-- Pokedex schema.
--
-- The data is highly relational (types, attacks and evolutions are shared by
-- many pokemons) and the API filters on those relations, so it is normalized.

CREATE TABLE types (
    name text PRIMARY KEY
);

CREATE TABLE pokemons (
    id                 integer          PRIMARY KEY,           -- Pokedex number
    name               text             NOT NULL,
    classification     text             NOT NULL,
    weight_min_kg      double precision NOT NULL,
    weight_max_kg      double precision NOT NULL,
    height_min_m       double precision NOT NULL,
    height_max_m       double precision NOT NULL,
    flee_rate          double precision NOT NULL,
    max_cp             integer          NOT NULL,
    max_hp             integer          NOT NULL,
    class              text             CHECK (class IN ('LEGENDARY', 'MYTHIC')), -- NULL for regular pokemons
    capture_area       text,
    evolution_candies  integer,                                -- candy cost to evolve, NULL if it cannot evolve
    evolution_candy_name text
);
CREATE UNIQUE INDEX pokemons_name_lower_idx ON pokemons (lower(name));

-- A pokemon has 1..2 types; slot keeps the order (primary type first).
CREATE TABLE pokemon_types (
    pokemon_id integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    type       text    NOT NULL REFERENCES types (name),
    slot       integer NOT NULL,
    PRIMARY KEY (pokemon_id, type)
);
CREATE INDEX pokemon_types_type_idx ON pokemon_types (type);

CREATE TABLE pokemon_weaknesses (
    pokemon_id integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    type       text    NOT NULL REFERENCES types (name),
    PRIMARY KEY (pokemon_id, type)
);
CREATE INDEX pokemon_weaknesses_type_idx ON pokemon_weaknesses (type);

CREATE TABLE pokemon_resistances (
    pokemon_id integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    type       text    NOT NULL REFERENCES types (name),
    PRIMARY KEY (pokemon_id, type)
);
CREATE INDEX pokemon_resistances_type_idx ON pokemon_resistances (type);

-- Attacks are shared between pokemons (e.g. "Tackle").
CREATE TABLE attacks (
    id       serial PRIMARY KEY,
    name     text    NOT NULL,
    category text    NOT NULL CHECK (category IN ('fast', 'special')),
    type     text    NOT NULL REFERENCES types (name),
    damage   integer NOT NULL,
    UNIQUE (name, category)
);

CREATE TABLE pokemon_attacks (
    pokemon_id integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    attack_id  integer NOT NULL REFERENCES attacks (id),
    PRIMARY KEY (pokemon_id, attack_id)
);

-- One row per "pokemon_id can evolve (eventually) into evolves_to_id".
-- The "previous evolutions" of a pokemon are the reverse lookup, so the
-- relationship is stored once.
CREATE TABLE pokemon_evolutions (
    pokemon_id    integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    evolves_to_id integer NOT NULL REFERENCES pokemons (id) ON DELETE CASCADE,
    PRIMARY KEY (pokemon_id, evolves_to_id),
    CHECK (pokemon_id <> evolves_to_id)
);
CREATE INDEX pokemon_evolutions_to_idx ON pokemon_evolutions (evolves_to_id);

-- The challenge has no notion of users, so favorites are global.
-- Adding a user_id to the key is the natural evolution once there is auth.
CREATE TABLE favorites (
    pokemon_id integer     PRIMARY KEY REFERENCES pokemons (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
