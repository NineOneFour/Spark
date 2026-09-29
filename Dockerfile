# Spark in one container: the web app, plus the collector on a loop.
# See INSTALL.md for run flags.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY collector/ collector/
COPY web/ web/
RUN cd collector && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/collector . \
 && cd ../web && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/web .

FROM alpine:3.20
COPY --from=build /out/ /usr/local/bin/
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
ENV SPARK_DATA_DIR=/projects \
    SPARK_ADDR=:8080 \
    TARGET_DIR=/projects \
    SCAN_ROOT=/sources \
    MACHINE_ID=local \
    COLLECT_INTERVAL=900
VOLUME /projects
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
