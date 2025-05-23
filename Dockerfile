FROM golang:latest AS builder

WORKDIR /app

COPY go.mod ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o mcp-server-kintone


ARG BASE_IMAGE
FROM ${BASE_IMAGE:-alpine:latest}

COPY --from=builder /app/mcp-server-kintone /

ENTRYPOINT ["/mcp-server-kintone"]
