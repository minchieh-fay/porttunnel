FROM hub.hitry.io/base/go:1.24.3 AS go
WORKDIR $GOPATH/src
ENV CGO_ENABLED 0
ENV GO111MODULE on
COPY . .
RUN go build -o app -ldflags "-s -w"

FROM hub.hitry.io/hitry/anolisos:h8.6.266674
COPY --from=go /go/src/app .
ENTRYPOINT ["./app"]