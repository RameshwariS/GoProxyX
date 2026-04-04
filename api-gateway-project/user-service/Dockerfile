FROM golang:1.20-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/user-service .

# final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/user-service /app/user-service
EXPOSE 3002
CMD ["./user-service"]
