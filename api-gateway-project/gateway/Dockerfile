FROM golang:1.20-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gateway .

# final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/gateway /app/gateway
EXPOSE 3000
CMD ["./gateway"]
