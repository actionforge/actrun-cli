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
LABEL org.opencontainers.image.version={{img.version}}
LABEL org.opencontainers.image.source={{img.source}}

COPY --from=build /app/bin /bin

ENTRYPOINT ["/bin/actrun"]
