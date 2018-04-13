FROM golang:1.10-alpine AS build
RUN apk add --no-cache git make curl \
  && (curl https://glide.sh/get | sh)

RUN mkdir -p /go/src/github.com/sstarcher/job-reaper
WORKDIR /go/src/github.com/sstarcher/job-reaper/

ENV CGO_ENABLED=0
ENV GOOS=linux

COPY glide.* /go/src/github.com/sstarcher/job-reaper/
RUN glide install

COPY . .
RUN  make build

FROM scratch
COPY --from=build /tmp /tmp
COPY --from=build /go/src/github.com/sstarcher/job-reaper/build/job-reaper /
COPY --from=build /go/src/github.com/sstarcher/job-reaper/myconfig.yaml /config.yaml

ENTRYPOINT ["/job-reaper"]
