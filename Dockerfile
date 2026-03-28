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

FROM alpine:3.22.0

LABEL org.opencontainers.image.title="Graph Runner"
LABEL org.opencontainers.image.description="Execution runtime for action graphs."
ARG IMG_VERSION=dev
ARG IMG_SOURCE=https://github.com/actionforge/actrun-cli

LABEL org.opencontainers.image.version=${IMG_VERSION}
LABEL org.opencontainers.image.source=${IMG_SOURCE}

COPY --from=build /app/bin /bin

ENTRYPOINT ["/bin/actrun"]
