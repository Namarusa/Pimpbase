# Pimpbase in a container: the same single binary, built static, on a
# distroless base (CA certificates and time zones, no shell).
#
#   docker build -t pimpbase .
#   docker run -p 8080:8080 -v pimpbase-data:/data pimpbase
#
# The builder UI is committed in web/builder/dist, so no Node step is
# needed; run "make ui" first if you changed it.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/pimpbase ./cmd/pimpbase \
 && mkdir -p /out/data /out/backups /out/files

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pimpbase /usr/local/bin/pimpbase
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build --chown=nonroot:nonroot /out/backups /backups
# Shared uploads folder for --files dir:/files (deploy/compose.cluster.yml).
# It must exist in the image so a new named volume mounted here is owned by
# the nonroot user; otherwise every upload fails with permission denied.
COPY --from=build --chown=nonroot:nonroot /out/files /files
ENV PIMPBASE_ADDR=:8080 \
    PIMPBASE_DATA=/data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s CMD ["pimpbase", "healthcheck"]
ENTRYPOINT ["pimpbase"]
CMD ["serve"]
