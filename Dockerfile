# Spark in one image: the web app, plus everything the host needs (collector,
# skills, setup script, instructions), copied into SparkRoot on start.
# See INSTALL.md for run flags.
# The build stage runs on the builder's own platform and cross-compiles, so a
# multi-platform build doesn't emulate the Go compiler.
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY web/ web/
RUN cd web && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/web .

FROM alpine:3.24.2
COPY --from=build /out/web /usr/local/bin/web
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY collector/collector.py setup.sh INSTALL.md /usr/local/share/spark/
COPY skill/ /usr/local/share/spark/Skill/
COPY handoff-skill/ /usr/local/share/spark/HandoffSkill/
RUN chmod 755 /usr/local/share/spark/collector.py /usr/local/share/spark/setup.sh \
 && chmod -R a+rX /usr/local/share/spark
ENV SPARK_ROOT=/spark \
    SPARK_ADDR=:9140
VOLUME /spark
EXPOSE 9140
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
