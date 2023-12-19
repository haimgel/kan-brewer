# Build stage (native architecture)
FROM --platform=${BUILDPLATFORM} golang:1.21.5-alpine3.19 AS builder

WORKDIR /app
COPY . .

ARG TARGETPLATFORM
ARG BUILDPLATFORM

RUN apk add --no-cache git file

RUN echo "Running on $BUILDPLATFORM, building for $TARGETPLATFORM" && \
    case "${TARGETPLATFORM}" in \
    "linux/amd64")  GOARCH="amd64" ;; \
    "linux/arm64")  GOARCH="arm64" ;; \
    "linux/386")    GOARCH="386" ;; \
    "linux/arm/v7") GOARCH="arm" GOARM="7" ;; \
    "linux/arm/v6") GOARCH="arm" GOARM="6" ;; \
    *)              echo "Unsupported platform ${TARGETPLATFORM}" && exit 1 ;; \
    esac && \
    VERSION=$(git describe --tags --always --match 'v*' --dirty='*') && \
    COMMIT=$(git rev-parse --short HEAD) && \
    COMMIT_DATE=$(git log -1 --format=%cI) && \
    echo "Building version '${VERSION}' commit '${COMMIT}' date '${COMMIT_DATE}'" && \
    LD_FLAGS="-s -w -X github.com/haimgel/kan-brewer/internal/config.release=${VERSION} -X github.com/haimgel/kan-brewer/internal/config.commit=${COMMIT} -X github.com/haimgel/kan-brewer/internal/config.date=${COMMIT_DATE}" && \
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" go build -ldflags "${LD_FLAGS}" -o kan-brewer cmd/kan-brewer.go && \
    file kan-brewer

# Run stage (target architecture)
FROM scratch
COPY --from=builder /app/kan-brewer /kan-brewer
CMD ["/kan-brewer"]

LABEL org.opencontainers.image.title=Kan-brewer
LABEL org.opencontainers.image.description="Kan-brewer is a backup scheduler for Kanister"
LABEL org.opencontainers.image.source=https://github.com/haimgel/kan-brewer
LABEL org.opencontainers.image.licenses=Apache-2.0
