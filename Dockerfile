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

RUN cd cmd/telegram && make linux

# final stage
FROM alpine:latest
RUN apk add --no-cache tzdata ca-certificates
COPY --from=builder /src/cmd/telegram/fam100 fam100
CMD ["./fam100"]
