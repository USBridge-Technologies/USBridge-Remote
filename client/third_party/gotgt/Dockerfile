FROM debian:buster
MAINTAINER Lei Xue <carmark.dlut@gmail.com>

# setup golang environment
ENV GOLANG_VERSION 1.14.4
ENV GOLANG_DOWNLOAD_URL https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz
ENV GOLANG_DOWNLOAD_SHA256 aed845e4185a0b2a3c3d5e1d0a35491702c55889192bb9c30e67a3de6849c067

RUN apt-get update && apt-get install -y curl automake gcc make libcephfs-dev librbd-dev librados-dev
RUN curl -fsSL "$GOLANG_DOWNLOAD_URL" -o golang.tar.gz \
	&& echo "$GOLANG_DOWNLOAD_SHA256  golang.tar.gz" | sha256sum -c - \
	&& tar -C /usr/local -xzf golang.tar.gz \
	&& rm golang.tar.gz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
WORKDIR $GOPATH

RUN mkdir -p /go/src/github.com/gostor/gotgt
ADD . /go/src/github.com/gostor/gotgt
WORKDIR ${GOPATH}/src/github.com/gostor/gotgt
RUN make

EXPOSE 23457
EXPOSE 3260
CMD ["./gotgt", "daemon"]

RUN apt-get purge -y curl automake gcc make
RUN apt-get autoremove -y
