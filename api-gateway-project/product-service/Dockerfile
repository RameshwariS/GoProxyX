FROM golang:1.20-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/item-service .

# final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/item-service /app/item-service
EXPOSE 3001
CMD ["./item-service"]
