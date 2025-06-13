FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o time-automation .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/time-automation .
COPY --from=builder /app/.env.example .env

# Set timezone to Europe/Berlin
ENV TZ=Europe/Berlin
RUN ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime

CMD ["./time-automation"]
