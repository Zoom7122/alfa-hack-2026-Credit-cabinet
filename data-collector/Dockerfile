FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /data-collector ./cmd/data-collector

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /data-collector /data-collector
ENV GIN_MODE=release HTTP_ADDR=:8081
EXPOSE 8081
ENTRYPOINT ["/data-collector"]
