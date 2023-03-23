FROM golang:alpine3.12 as golang
RUN apk add bash make git zip tzdata ca-certificates gcc musl-dev
WORKDIR /app
COPY . .
RUN make linux

FROM alpine:3.12
# Dependencies
RUN apk --no-cache add tzdata ca-certificates musl curl

RUN echo "**** install Python ****" && \
    apk add --no-cache python3 && \
    if [ ! -e /usr/bin/python ]; then ln -sf python3 /usr/bin/python ; fi && \
    \
    echo "**** install pip ****" && \
    python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --no-cache --upgrade pip setuptools wheel && \
    if [ ! -e /usr/bin/pip ]; then ln -s pip3 /usr/bin/pip ; fi

# where application lives
WORKDIR /app
# Copy the products
COPY --from=golang /app/bin .
# Copy script
COPY ./scripts/mongoshake_mon.py .
RUN pip install boto3 requests

# metrics
EXPOSE 9100 9101 9200
ENTRYPOINT ["/app/collector", "--conf=/app/conf/collector.conf", "--verbose=2", "&&", "python", "/app/mongoshake_mon.py"]