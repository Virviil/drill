FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /drill ./cmd/drill

FROM alpine:3.20
# docker CLI talks to the host daemon through the mounted socket
RUN apk add --no-cache docker-cli ca-certificates
COPY --from=build /drill /usr/local/bin/drill
COPY tasks /app/tasks
COPY plan.yaml /app/plan.yaml
WORKDIR /app
ENV DRILL_CONTENT=/app DRILL_HOSTKEY=/data/drill_ed25519
EXPOSE 2222
ENTRYPOINT ["drill"]
