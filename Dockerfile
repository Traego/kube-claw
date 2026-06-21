# kube-claw controller image. CGO_ENABLED=0 works because the SQLite driver
# (modernc.org/sqlite) is pure Go. distroless/static:nonroot satisfies GKE
# Autopilot's non-root requirement.
FROM golang:1.26 AS build
WORKDIR /src
# Pin the toolchain to the base image's Go (no surprise auto-downloads in CI).
ENV GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/claw-controller ./cmd/claw-controller

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/claw-controller /claw-controller
USER 65532:65532
ENTRYPOINT ["/claw-controller"]
