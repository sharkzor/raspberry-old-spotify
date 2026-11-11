# Build environment for the SpotifyPi Buildroot image.
# x86_64 because Buildroot's prebuilt (Bootlin) toolchain is x86_64-only;
# on Apple Silicon Docker Desktop runs this through Rosetta.
FROM --platform=linux/amd64 debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        bash bc binutils build-essential bzip2 ca-certificates cpio diffutils \
        file findutils g++ gcc git gzip libncurses-dev locales make patch perl \
        python3 rsync sed tar unzip wget xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen

ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# Optional extra CA certificates (e.g. a corporate TLS-inspecting proxy):
# drop PEM files named *.crt into docker/extra-ca/.
COPY extra-ca/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    CARGO_HTTP_CAINFO=/etc/ssl/certs/ca-certificates.crt \
    GIT_SSL_CAINFO=/etc/ssl/certs/ca-certificates.crt
RUN useradd -m -u 1000 builder && mkdir -p /work /out && chown builder:builder /work /out
USER builder
WORKDIR /work
