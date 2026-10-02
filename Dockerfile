# Spark in one image: the web app, plus everything the host needs (collector,
# skills, setup script, instructions), copied into SparkRoot on start.
# See INSTALL.md for run flags.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY web/ web/
RUN cd web && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/web .

FROM alpine:3.20
COPY --from=build /out/web /usr/local/bin/web
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY collector/collector.py setup.sh INSTALL.md /usr/local/share/spark/
COPY skill/ /usr/local/share/spark/Skill/
COPY handoff-skill/ /usr/local/share/spark/HandoffSkill/
RUN chmod 755 /usr/local/share/spark/collector.py /usr/local/share/spark/setup.sh \
 && chmod -R a+rX /usr/local/share/spark
ENV SPARK_ROOT=/spark \
    SPARK_ADDR=:8080
VOLUME /spark
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
