# Sandbox image for Go solutions: toolchain + warm build cache for the stdlib,
# so a typical solution compiles in ~1 s without network.
FROM golang:1.25-alpine
ENV GOCACHE=/gocache GOTOOLCHAIN=local GOFLAGS=-mod=mod CGO_ENABLED=0 HOME=/tmp
RUN mkdir /warm && cd /warm \
 && printf 'module warm\n\ngo 1.25\n' > go.mod \
 && printf 'package main\n\nimport (\n_ "bufio"\n_ "container/heap"\n_ "container/list"\n_ "encoding/json"\n_ "errors"\n_ "fmt"\n_ "maps"\n_ "math"\n_ "math/bits"\n_ "os"\n_ "slices"\n_ "sort"\n_ "strconv"\n_ "strings"\n_ "sync"\n_ "time"\n_ "unicode"\n)\n\nfunc main() {}\n' > main.go \
 && go build -o /dev/null . \
 && cd / && rm -rf /warm && chmod -R a+rwX /gocache
