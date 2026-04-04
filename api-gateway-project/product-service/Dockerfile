FROM golang:1.20-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/product-service .

# final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/product-service /app/product-service
EXPOSE 3001
CMD ["./product-service"]
