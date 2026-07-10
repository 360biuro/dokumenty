# --- Etap 1: budowanie aplikacji Go ---
FROM golang:1.22-bookworm AS build
WORKDIR /app
COPY go.mod ./
COPY main.go ./
# go mod tidy pobiera i zapisuje wymagane zależności (goldmark) - wymaga
# dostępu do sieci podczas budowania obrazu.
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /app/dokumenty-generator ./main.go

# --- Etap 2: obraz uruchomieniowy ---
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        wkhtmltopdf \
        xvfb \
        fonts-dejavu-core \
        fontconfig \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /app/dokumenty-generator ./dokumenty-generator

ENV PORT=8080
ENV DOCS_DIR=/app/pliki

EXPOSE 8080
VOLUME ["/app/pliki"]

ENTRYPOINT ["./dokumenty-generator"]
