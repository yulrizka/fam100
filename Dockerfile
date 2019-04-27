# build stage for dealing with go module caching
FROM golang:1.12.4 AS builder_base
WORKDIR /src

COPY go.mod .
COPY go.sum .

RUN go mod download


# build the binary
FROM builder_base AS builder
WORKDIR /src
ADD . /src

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go install -a -tags netgo -ldflags '-w -extldflags "-static"' ./cmd/telegram


# final stage
FROM alpine:latest
RUN apk add --no-cache tzdata
COPY --from=builder /go/bin/telegram fam100
CMD ["./fam100"]
