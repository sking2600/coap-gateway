FROM golang:latest as builder
WORKDIR /go/src/github.com/go-ocf/coap-gateway
RUN go get -d -v github.com/go-ocf/go-coap
RUN go get -d -v github.com/ugorji/go/codec
COPY ./*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o coap-gateway .

FROM scratch
WORKDIR /root/
COPY --from=builder /go/src/github.com/go-ocf/coap-gateway/coap-gateway .
CMD ["./coap-gateway"]
