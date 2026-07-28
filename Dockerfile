# Stage 1
# Go img as builder enviroment
FROM golang:1.25-alpine AS builder

# set working directory to the root of the repo, happens by default if not set
WORKDIR /

# copy dependencies into ./ of the container
COPY go.mod go.sum ./
RUN go mod download

# copy source code
COPY . .

# compile the GO app from the entrypoint ./cmd/main.go not a binary exe and save it at /bin/proxy
RUN go build -o /bin/proxy ./cmd/main.go

# Stage 2
# starts a new container
FROM alpine:latest

# from prev builder stage, copy the binary from /bin/proxy
COPY --from=builder /bin/proxy .

EXPOSE 80

CMD ["./proxy"]
