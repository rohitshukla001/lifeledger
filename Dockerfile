FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w -X github.com/rohitshukla001/lifeledger/internal/version.Version=${VERSION} -X github.com/rohitshukla001/lifeledger/internal/version.Commit=${COMMIT}" \
      -o /out/lifeledger ./cmd/lifeledger \
 && mkdir -p /out/data

FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=server /out/lifeledger /usr/local/bin/lifeledger
COPY --from=server --chown=nonroot:nonroot /out/data /data
COPY --from=web /src/web/dist/browser /srv/web
ENV LIFELEDGER_ENV=prod \
    LIFELEDGER_ADDR=:8080 \
    LIFELEDGER_DB_PATH=/data/lifeledger.db \
    LIFELEDGER_WEB_DIR=/srv/web
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/lifeledger"]
CMD ["serve"]
