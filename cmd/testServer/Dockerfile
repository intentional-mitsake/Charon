# Stage 1

# Build the Go app
FROM golang:1.25-alpine AS builder

WORKDIR /

COPY go.mod go.sum ./
RUN go mod download

# copy the code from cmd/testServer of the repo to ./cmd/testServer of the container
# used this instead of COPY . . as context was set to . in compose, meaning the root of the repo is context
# so using COPY .. would copy the whole repo into . of the container, 
# dont need to all that cuz cmd/testServer/main.go is self contained, no imports from the repo 
COPY cmd/testServer/ ./cmd/testServer

RUN go build -o /bin/backend ./cmd/testServer/main.go

# Stage 2

FROM alpine:latest

COPY --from=builder /bin/backend .

EXPOSE ${UPSTREAM_PORT}

CMD ["./backend"]