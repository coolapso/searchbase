FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /fixture ./cmd/fixture
FROM scratch
COPY --from=builder /fixture /fixture
ENTRYPOINT ["/fixture"]
