ARG GO_VERSION=1.26.6
FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/launch ./cmd/launch

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/launch /launch
COPY LICENSE /LICENSE
COPY third_party /third_party
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/launch"]
CMD ["serve"]
