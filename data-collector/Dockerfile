FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# три бинаря одного модуля: API, publisher outbox → Kafka, worker сбора
RUN for cmd in data-collector:data-collector data-collector-publisher:outbox-publisher data-collector-worker:worker; do \
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/${cmd%%:*}" "./cmd/${cmd#*:}" || exit 1; \
    done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /
ENV GIN_MODE=release HTTP_ADDR=:8081
EXPOSE 8081
# publisher и worker запускаются из того же образа: entrypoint /data-collector-publisher, /data-collector-worker
ENTRYPOINT ["/data-collector"]
