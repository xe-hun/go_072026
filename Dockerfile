FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/notes-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/notes-worker ./cmd/worker

FROM alpine:3.22

RUN adduser -D -H appuser
WORKDIR /app
COPY --from=build /out/notes-api /app/notes-api
COPY --from=build /out/notes-worker /app/notes-worker
USER appuser

EXPOSE 8080
CMD ["/app/notes-api"]

