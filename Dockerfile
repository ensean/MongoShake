FROM golang:alpine3.12 as golang
RUN apk add bash make git zip tzdata ca-certificates gcc musl-dev
WORKDIR /app
COPY . .
RUN make linux

FROM alpine:3.12
# Dependencies
RUN apk --no-cache add tzdata ca-certificates musl curl
# where application lives
WORKDIR /app
# Copy the products
COPY --from=golang /app/bin .
# metrics
EXPOSE 9100 9101 9200
ENTRYPOINT ["/app/collector", "--conf=/app/conf/collector.conf", "--verbose=2"]