FROM --platform=$BUILDPLATFORM golang:1.18 as builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build

FROM ubuntu

# Copy in the binary
COPY --from=builder /src /

EXPOSE 8545 8080

CMD ["./ethermint-proxy"]