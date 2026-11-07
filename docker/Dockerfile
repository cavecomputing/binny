# The build context is the repo root, not this folder, so COPY paths start there.
# compose.yml sets that up; to build by hand: docker build -f docker/Dockerfile .
FROM golang:1.27.3-trixie AS build

WORKDIR /src
COPY go.mod go.sum main.go VERSION ./
COPY internal/ internal/
COPY web/ web/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /binny .

# Debian rather than an empty image for the entrypoint's shell and setpriv; the CA certificates
# are for upload from link's https.
FROM debian:trixie-slim

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /binny /app/binny
COPY docker/entrypoint.sh /app/

EXPOSE 5000
ENV BINNY_DATA_DIR=/data

# No curl in the image: bash asks for the sign-in page itself.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD bash -c 'exec 3<>/dev/tcp/127.0.0.1/5000 && printf "GET /login HTTP/1.0\r\n\r\n" >&3 && grep -q " 200 " <&3'

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/binny", "-host", "0.0.0.0"]
