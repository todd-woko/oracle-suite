# TODO: Unify this dockerfile into a single file that is parameterized by the app name.
FROM golang:1.20-alpine as builder
RUN apk --no-cache add git gcc libc-dev linux-headers

WORKDIR /app
COPY . .
RUN go mod vendor && mkdir -p dist

ARG APP_VERSION
RUN CGO_ENABLED=0 go build -v \
    -ldflags "-X github.com/chronicleprotocol/oracle-suite.Version=$APP_VERSION" \
    -o ./dist/spectre \
    ./cmd/spectre

# ----------------------------------------------------------------------------------------
#FROM alpine:3.16
FROM gcr.io/distroless/static-debian11

WORKDIR /root
COPY --from=builder /app/dist/* /usr/local/bin/
COPY ./config-*.hcl ./cmd/spectre/config.hcl /usr/share/oracle-suite/

ENTRYPOINT ["/usr/local/bin/spectre"]
CMD ["-c", "/usr/share/oracle-suite/config.hcl", "run"]

# libp2p
EXPOSE 8000

# webapi
EXPOSE 8080
