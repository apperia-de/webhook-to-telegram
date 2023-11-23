# syntax=docker/dockerfile:1
FROM golang:1.21-alpine AS build-stage
# Set destination for COPY
WORKDIR /app

# Download Go modules
# can be skipped, since we use the vendor folder
#COPY go.mod go.sum ./
#RUN go mod download
# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/engine/reference/builder/#copy
COPY . ./
# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/wh2t cmd/main.go

# Deploy the application binary into a lean image
#FROM gcr.io/distroless/base-debian12 AS build-release-stage
FROM gcr.io/distroless/static-debian12 AS build-release-stage

WORKDIR /

COPY --from=build-stage /app/wh2t /app/config.yml ./

USER nonroot:nonroot

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/engine/reference/builder/#expose
EXPOSE 8080

# Run
CMD ["/wh2t"]