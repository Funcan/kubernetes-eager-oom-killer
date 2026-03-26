FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /eager-oom-killer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /eager-oom-killer /eager-oom-killer
ENTRYPOINT ["/eager-oom-killer"]
