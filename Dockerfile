FROM golang:1.24.2-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/kite-mcp-server .

EXPOSE 8080

ENTRYPOINT ["/app/kite-mcp-server"] 