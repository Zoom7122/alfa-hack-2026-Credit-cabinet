FROM golang:1.26.8-bookworm AS build
WORKDIR /src/credit-engine

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY contracts/ ./contracts/
COPY models/ ./models/
COPY migrations/ ./migrations/
COPY docs/02_inputs_and_scoring.md ./docs/02_inputs_and_scoring.md
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/modelgen ./cmd/modelgen \
    && /out/modelgen check \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scoring-service ./cmd/scoring-service \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bank-state-service ./cmd/bank-state-service \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin-service ./cmd/admin-service \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin-migrate ./cmd/admin-migrate \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin-password ./cmd/admin-password

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/ /
COPY config/ ./config/

EXPOSE 8082 8083 8084
# Select /bank-state-service or /admin-service for the other HTTP processes.
ENTRYPOINT ["/scoring-service"]
