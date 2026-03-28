##
## Build
##

FROM golang:1.25.0-alpine3.22 AS build

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . ./

RUN go build -o ./bin/actrun

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
