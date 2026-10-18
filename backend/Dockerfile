FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Сгенерированный код в git не хранится, собираем его здесь
RUN GOBIN=/usr/local/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
 && GOBIN=/usr/local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
COPY api ./api
COPY backend ./backend
RUN cd backend && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml && sqlc generate
RUN CGO_ENABLED=0 go build -o /out/api ./backend/cmd/api
RUN CGO_ENABLED=0 GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM gcr.io/distroless/static-debian12 AS api
COPY --from=build /out/api /api
ENTRYPOINT ["/api"]

FROM gcr.io/distroless/static-debian12 AS migrate
COPY --from=build /out/goose /goose
COPY backend/migrations /migrations
ENTRYPOINT ["/goose", "-dir", "/migrations", "postgres"]
