# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT

ARG B19_UBUNTU_BASE_IMAGE=registry.invalid/b19/ubuntu:resolute
ARG B19_GO_BASE_IMAGE=registry.invalid/b19/go:latest
ARG B19_UBUNTU_SERIES=resolute

FROM ${B19_GO_BASE_IMAGE} AS damian-buho-report-relay-builder

ARG B19_COLOR
ARG B19_FETCH_DOCKER_CACHE
ARG B19_FETCH_LOCAL_CACHE
ARG B19_OFFGRID_MODE
ARG B19_VERBOSITY
ARG LANG=""
ARG M6E_AI=N
ARG M6E_APT_CACHE_HOST=""
ARG M6E_APT_CACHE_PORT=""
ARG M6E_BUILD_DEBUG=""
ARG M6E_NEAR_CACHE_HOST=""
ARG M6E_NAMESPACE
ARG M6E_PROJECT
ARG M6E_VERSION
ARG TARGETARCH

COPY --chown=${B19_UID}:${B19_GID} .container/compile-go/  /
COPY --chown=${B19_UID}:${B19_GID} go.mod go.sum main.go   ${B19_HOME}/
COPY --chown=${B19_UID}:${B19_GID} internal/               ${B19_HOME}/internal/

USER 0

WORKDIR ${B19_HOME}

RUN --mount=type=bind,from=fetch,source=.,target=/fetch                                           \
    --mount=type=cache,target=${B19_DOWNLOAD_PATH},sharing=shared                                 \
    --mount=type=cache,target=${GOCACHE},sharing=locked                                           \
    --mount=type=cache,target=${GOMODCACHE},sharing=locked                                        \
    --mount=type=cache,id=apt-cache-${B19_UBUNTU_SERIES}-${TARGETARCH},target=/var/cache/apt,sharing=shared     \
    --mount=type=cache,id=apt-lists-${B19_UBUNTU_SERIES}-${TARGETARCH},target=/var/lib/apt,sharing=shared       \
    --mount=type=tmpfs,target=${B19_TEMP_PATH}                                                      \
    build-stage compile-go

# hadolint ignore=DL3066 # B19_UID comes from the root
USER ${B19_UID}

FROM ${B19_UBUNTU_BASE_IMAGE} AS damian-buho-report-relay

ARG B19_COLOR
ARG B19_FETCH_DOCKER_CACHE
ARG B19_FETCH_LOCAL_CACHE
ARG B19_OFFGRID_MODE
ARG B19_VERBOSITY
ARG LANG=""
ARG M6E_AI=N
ARG M6E_APT_CACHE_HOST=""
ARG M6E_APT_CACHE_PORT=""
ARG M6E_BUILD_DEBUG=""
ARG M6E_NAMESPACE
ARG M6E_NEAR_CACHE_HOST=""
ARG M6E_PROJECT
ARG TARGETARCH

ENV M6E_VERSION=${M6E_VERSION}                \
    REPORT_RELAY_LOG_LEVEL=info               \
    REPORT_RELAY_HTTP_PORT=8080               \
    REPORT_RELAY_ADMIN_PORT=8081              \
    REPORT_RELAY_MAX_BODY_BYTES=65536         \
    REPORT_RELAY_MAX_JSON_DEPTH=32            \
    REPORT_RELAY_MAX_ARRAY_ITEMS=512          \
    REPORT_RELAY_RATE_LIMIT_RPS=20            \
    REPORT_RELAY_RATE_LIMIT_BURST=40          \
    REPORT_RELAY_KEEP_QUERY=false             \
    REPORT_RELAY_TRUST_PROXY=false            \
    REPORT_RELAY_TRUSTED_PROXIES=""           \
    REPORT_RELAY_EXPORT_TIMEOUT=10s           \
    REPORT_RELAY_SHUTDOWN_TIMEOUT=15s         \
     REPORT_RELAY_ENABLE_REPORTING_API=true    \
     REPORT_RELAY_ENABLE_CSP=true              \
     REPORT_RELAY_ENABLE_TLSRPT=true           \
     REPORT_RELAY_ENABLE_EXPECT_CT=true        \
     REPORT_RELAY_ENABLE_HPKP=true

COPY --chown=${B19_UID}:${B19_GID} .container/base/ /
COPY --from=damian-buho-report-relay-builder /export /

USER 0

WORKDIR ${B19_HOME}

RUN --mount=type=bind,from=fetch,source=.,target=/fetch                                           \
    --mount=type=cache,target=${B19_DOWNLOAD_PATH},sharing=shared                                 \
    --mount=type=cache,id=apt-cache-${B19_UBUNTU_SERIES}-${TARGETARCH},target=/var/cache/apt,sharing=shared     \
    --mount=type=cache,id=apt-lists-${B19_UBUNTU_SERIES}-${TARGETARCH},target=/var/lib/apt,sharing=shared       \
    --mount=type=tmpfs,target=${B19_TEMP_PATH}                                                                   \
    build-stage base

# hadolint ignore=DL3066 # B19_UID comes from the root
USER ${B19_UID}

COPY --chown=${B19_UID}:${B19_GID} .container/user/ /

ARG M6E_VERSION
RUN --mount=type=bind,from=fetch,source=.,target=/fetch                                             \
    --mount=type=cache,target=${B19_DOWNLOAD_PATH},sharing=shared,uid=${B19_UID},gid=${B19_GID}     \
    --mount=type=tmpfs,target=${B19_TEMP_PATH}                                                      \
    build-stage user

# ENTRYPOINT ["entrypoint.d"] is inherited
# HEALTHCHECK CMD ["healthcheck.d"] is inherited
# Don't use CMD ["sleep", "infinity"] here

# Enable Traefik Docker Discovery
LABEL traefik.enable=true
LABEL traefik.http.routers.report-relay.rule="Host(`report-relay.docker.localhost`)"
LABEL traefik.http.routers.report-relay.entrypoints=web,websecure
LABEL traefik.http.routers.report-relay.middlewares=redirect-to-https@file
LABEL traefik.http.services.report-relay.loadbalancer.server.port=${REPORT_RELAY_HTTP_PORT}

