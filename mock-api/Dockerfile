FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mock-api ./cmd/mock-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /mock-api /mock-api
ENV GIN_MODE=release HTTP_ADDR=:8090
EXPOSE 8090
ENTRYPOINT ["/mock-api"]
