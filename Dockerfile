# Bubibase in a container: the same single binary, built static, on a
# distroless base (CA certificates and time zones, no shell).
#
#   docker build -t bubibase .
#   docker run -p 8080:8080 -v bubibase-data:/data bubibase
#
# The builder UI is committed in web/builder/dist, so no Node step is
# needed; run "make ui" first if you changed it.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/bubibase ./cmd/bubibase \
 && mkdir -p /out/data /out/backups

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bubibase /usr/local/bin/bubibase
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build --chown=nonroot:nonroot /out/backups /backups
ENV BUBIBASE_ADDR=:8080 \
    BUBIBASE_DATA=/data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s CMD ["bubibase", "healthcheck"]
ENTRYPOINT ["bubibase"]
CMD ["serve"]
