# Test fixture only. Official release binary, independent of container registries.
FROM alpine:3.24
ARG TARGETARCH
ARG MINIO_RELEASE=RELEASE.2025-09-07T16-13-09Z
RUN apk add --no-cache ca-certificates curl \
    && curl -fsSL --retry 3 "https://github.com/minio/minio/releases/download/${MINIO_RELEASE}/minio.linux-${TARGETARCH}.${MINIO_RELEASE}" -o /usr/local/bin/minio \
    && curl -fsSL --retry 3 "https://github.com/minio/minio/releases/download/${MINIO_RELEASE}/minio.linux-${TARGETARCH}.${MINIO_RELEASE}.sha256sum" -o /tmp/minio.sha256sum \
    && cd /usr/local/bin \
    && mv minio "minio.${MINIO_RELEASE}" \
    && sha256sum -c /tmp/minio.sha256sum \
    && mv "minio.${MINIO_RELEASE}" minio \
    && chmod +x minio \
    && rm /tmp/minio.sha256sum
ENTRYPOINT ["minio"]
