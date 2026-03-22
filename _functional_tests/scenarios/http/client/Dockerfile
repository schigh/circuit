FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /client ./_functional_tests/scenarios/http/client

FROM alpine:3.19
COPY --from=build /client /client
CMD ["/client"]
