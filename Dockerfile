##
## Build
##

FROM golang:1.25.0 AS build

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . ./

RUN ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "x64") && \
    bash setup.sh linux "$ARCH" && \
    P4_INCLUDE="$(pwd)/p4api/include" && \
    if [ "$TARGETARCH" = "arm64" ]; then P4_LIB="$(pwd)/p4api/linux-aarch64/lib"; \
    else P4_LIB="$(pwd)/p4api/linux-x86_64/lib"; fi && \
    CGO_ENABLED=1 \
    CGO_CPPFLAGS="-I$P4_INCLUDE" \
    CGO_LDFLAGS="-L$P4_LIB -lp4api -lssl -lcrypto" \
    go build -tags=p4 -o ./bin/actrun

##
## Deploy
##

FROM ubuntu:24.04

LABEL org.opencontainers.image.title="Graph Runner"
LABEL org.opencontainers.image.description="Execution runtime for action graphs."
ARG IMG_VERSION=dev
ARG IMG_SOURCE=https://github.com/actionforge/actrun-cli

LABEL org.opencontainers.image.version=${IMG_VERSION}
LABEL org.opencontainers.image.source=${IMG_SOURCE}

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    locales \
    curl \
    wget \
    jq \
    zip \
    unzip \
    tar \
    xz-utils \
    python3 \
    libicu74 libssl3t64 \
    && PWSH_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "x64") \
    && curl -fsSL "https://github.com/PowerShell/PowerShell/releases/download/v7.5.5/powershell-7.5.5-linux-${PWSH_ARCH}.tar.gz" \
       -o /tmp/pwsh.tar.gz \
    && mkdir -p /opt/microsoft/powershell/7 \
    && tar -xzf /tmp/pwsh.tar.gz -C /opt/microsoft/powershell/7 \
    && chmod +x /opt/microsoft/powershell/7/pwsh \
    && ln -s /opt/microsoft/powershell/7/pwsh /usr/bin/pwsh \
    && rm /tmp/pwsh.tar.gz \
    && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen \
    && rm -rf /var/lib/apt/lists/*

ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

COPY --from=build /app/bin /bin

ENTRYPOINT ["/bin/actrun"]
