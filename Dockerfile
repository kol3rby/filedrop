FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /filedrop ./cmd/server

FROM gcr.io/distroless/static:nonroot
COPY --from=build /filedrop /filedrop
ENV ADDR=:8080 DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/filedrop"]
