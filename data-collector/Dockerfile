FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# бинари одного модуля: API, publisher outbox → Kafka, worker сбора, worker проверки подписи, worker оценки
RUN for cmd in data-collector:data-collector data-collector-publisher:outbox-publisher data-collector-worker:worker \
               data-collector-signature-worker:signature-worker data-collector-decision-worker:decision-worker; do \
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/bin/${cmd%%:*}" "./cmd/${cmd#*:}" || exit 1; \
    done && \
    mkdir -p /out/data/documents

FROM python:3.12-slim-bookworm
COPY financial_parser/requirements.txt /tmp/financial-requirements.txt
RUN pip install --no-cache-dir -r /tmp/financial-requirements.txt
COPY financial_parser/ /app/financial_parser/
WORKDIR /app
COPY --from=build /out/bin/ /
# папка файлов документов принадлежит nonroot: новый volume берёт владельца отсюда, иначе API не сможет писать
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 FINANCIAL_PARSER_PATH=/app/financial_parser/parser.py GIN_MODE=release HTTP_ADDR=:8081 DOCUMENTS_DIR=/data/documents
EXPOSE 8081
# publisher и worker'ы запускаются из того же образа: entrypoint /data-collector-publisher, /data-collector-worker,
# /data-collector-signature-worker
ENTRYPOINT ["/data-collector"]
