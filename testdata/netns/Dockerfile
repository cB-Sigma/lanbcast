# The Linux multi-LAN test (netns_linux_test.go) in a privileged
# container. From the module root:
#   docker build -t sigma-lanbcast-netns testdata/netns
#   docker run --rm --privileged -v "$PWD":/src sigma-lanbcast-netns
FROM golang:1.26-bookworm
RUN apt-get update \
 && apt-get install -y --no-install-recommends iproute2 procps \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
ENV SIGMA_NETNS=1 GOFLAGS=-buildvcs=false
CMD ["go", "test", "-count=1", "-race", "-v", "-run", "Netns|Responder|Probe", "./..."]
