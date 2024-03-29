# Build with the golang image
FROM golang:1.15-alpine AS build

# Add git
RUN apk add git

# Set workdir
WORKDIR /go/src/github.com/statcan/kubeflow-controller

# Add dependencies
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 go install .

# Generate final image
FROM alpine:3.15
RUN apk --update --no-cache add ca-certificates
COPY --from=build /go/bin/kubeflow-controller /usr/local/bin/kubeflow-controller
ENTRYPOINT [ "/usr/local/bin/kubeflow-controller" ]
