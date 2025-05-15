FROM hub.hitry.io/base/golang:1.15-alpine3.13 AS go
WORKDIR $GOPATH/src
ENV CGO_ENABLED 0
ENV GO111MODULE on
COPY . .
RUN go build -o app -ldflags "-s -w"

FROM hub.hitry.io/hitry/anolisos:h8.6.266674
COPY --from=go /go/src/app .
ENTRYPOINT ["./app"]