ARG GO_VERSION=1.26.6
FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/launch ./cmd/launch
RUN mkdir -p /out/data/uploads && chmod 0700 /out/data/uploads

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/launch /launch
COPY --from=build --chown=65532:65532 /out/data/ /var/lib/licentivo-launch/
COPY LICENSE /LICENSE
COPY third_party /third_party
USER 65532:65532
ENV LAUNCH_UPLOAD_DIR=/var/lib/licentivo-launch/uploads \
    TMPDIR=/var/lib/licentivo-launch/uploads
EXPOSE 8080
ENTRYPOINT ["/launch"]
CMD ["serve"]
