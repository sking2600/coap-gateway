FROM golang:alpine AS build-env
WORKDIR /app
ADD . /app
RUN apk update && apk add git
RUN go get -d -v

RUN cd /app && go build -o goapp

FROM alpine 
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=build-env /app/goapp /app

ENTRYPOINT ./goapp