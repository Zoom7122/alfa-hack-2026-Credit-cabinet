FROM golang:1.26.8-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY contracts/ ./contracts/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scoring-service ./cmd/scoring-service \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bank-state-service ./cmd/bank-state-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/ /
COPY config/ ./config/

EXPOSE 8082 8083
# Both services use this image; select /bank-state-service for MS2.
ENTRYPOINT ["/scoring-service"]
