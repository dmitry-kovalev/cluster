# STEP 1 build executable binary
FROM golang:alpine as builder
COPY . $GOPATH/src/dmitry-kovalev/cluster
WORKDIR $GOPATH/src/dmitry-kovalev/cluster
#build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o /go/bin/leader -v ./example/basic/leader.go

# STEP 2 build a small image
# start from scratch
FROM alpine

ARG APP_DIR=/leader/
WORKDIR $APP_DIR
# Copy our static executable
COPY --from=builder /go/bin/leader /leader/

CMD ["/leader/leader"]
