FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/item-service .

FROM alpine:3.20
# postgresql-client provides psql, used by entrypoint.sh to apply
# migrations and seed data automatically before the server starts.
RUN apk add --no-cache postgresql-client
WORKDIR /app
COPY --from=builder /out/item-service /app/item-service
COPY migrations ./migrations
COPY seed.sql .
COPY entrypoint.sh .
RUN chmod +x entrypoint.sh
EXPOSE 3001
ENTRYPOINT ["./entrypoint.sh"]
