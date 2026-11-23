FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && CGO_ENABLED=0 go build -o /out/seed ./cmd/seed

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/api /out/seed /app/
COPY pokemons.json /app/pokemons.json
WORKDIR /app
ENV POKEMON_FILE=/app/pokemons.json
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]
