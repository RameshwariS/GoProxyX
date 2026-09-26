FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/item-service .
 
FROM alpine:3.20
COPY --from=builder /out/item-service /item-service
EXPOSE 3003
ENTRYPOINT ["/item-service"]
