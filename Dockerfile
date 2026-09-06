FROM eclipse-temurin:25-jdk AS build
WORKDIR /src
COPY gradlew gradle.properties settings.gradle.kts build.gradle.kts ./
COPY gradle ./gradle
COPY src ./src
COPY upstream/backend ./upstream/backend
ARG UPSTREAM_REVISION
RUN --mount=type=cache,target=/root/.gradle \
    ./gradlew --no-daemon -PupstreamRevision="$UPSTREAM_REVISION" bootJar

FROM eclipse-temurin:25-jre
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash softhsm2 opensc openssl jq curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home wallet \
    && mkdir /data && chown wallet:wallet /data
ENV SOFTHSM2_CONF=/data/softhsm2.conf
WORKDIR /app
COPY --from=build /src/build/libs/german-wallet-local-backend-1.0.0.jar /app/backend.jar
COPY docker/provision.sh docker/entrypoint.sh /app/
USER wallet
EXPOSE 8080
ENTRYPOINT ["/bin/sh", "/app/entrypoint.sh"]
