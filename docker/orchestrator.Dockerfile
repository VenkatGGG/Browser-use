FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/orchestrator ./cmd/orchestrator

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=builder /out/orchestrator /orchestrator
EXPOSE 8080
ENTRYPOINT ["/orchestrator"]
