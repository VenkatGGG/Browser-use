ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/orchestrator ./cmd/orchestrator

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=builder /out/orchestrator /orchestrator
EXPOSE 8080
ENTRYPOINT ["/orchestrator"]
