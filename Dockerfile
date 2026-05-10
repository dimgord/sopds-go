# syntax=docker/dockerfile:1.7
#
# Minimal runtime image for sopds-go, designed for GoReleaser integration:
# binaries are built outside (cross-platform via Go) and COPYed in here,
# rather than compiled inside the image. Keeps the image small and the
# build fast (binary builds get cached by GoReleaser, image build is just
# a COPY).
#
# Base: distroless/base-debian12 — has glibc, CA certs, /etc/passwd with
# nobody/nonroot users, and nothing else. No shell, no package manager.
# Image size: ~25MB before binaries.
#
# What's NOT included in this default image:
#   - PostgreSQL (run separately, e.g. as a sibling container)
#   - Calibre (`ebook-convert`) — needed only for FB2 → MOBI; users who
#     need it should build a derivative image: `FROM ghcr.io/dimgord/sopds-go`
#     `RUN apt update && apt install -y calibre`. Calibre adds ~500MB.
#   - espeak-ng / Piper voice models — same story, build a derivative for TTS.
#
# Usage:
#   docker run -d \
#     -p 8081:8081 \
#     -v /path/to/library:/library:ro \
#     -v /path/to/config.yaml:/etc/sopds/config.yaml:ro \
#     ghcr.io/dimgord/sopds-go:latest

FROM gcr.io/distroless/base-debian12:nonroot

# Pre-built binaries from GoReleaser's docker context (CGO_ENABLED=0,
# static, ~10-15MB each). `sopds-tts` is intentionally not in the
# release matrix — it depends on CGO + libonnxruntime; users who need
# TTS either build it from source or use sopds-tts-rs/.
COPY sopds /usr/local/bin/sopds
COPY zipdupes /usr/local/bin/zipdupes

# Reference config — users should mount over /etc/sopds/config.yaml.
COPY config.yaml.example /etc/sopds/config.yaml.example

EXPOSE 8081

USER nonroot:nonroot

# `start` is the long-running server command; `-c` reads config from the
# mounted volume. Override CMD for one-shot operations like `migrate` /
# `scan` / `import-mysql`.
ENTRYPOINT ["/usr/local/bin/sopds"]
CMD ["start", "-c", "/etc/sopds/config.yaml"]
