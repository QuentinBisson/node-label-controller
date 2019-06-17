FROM golang:1.12 as builder

LABEL maintainer="Quentin Bisson <quentin.bisson@gmail.com>"

WORKDIR /src

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o node-label-controller .


FROM alpine 

WORKDIR /app

COPY --from=builder /src/node-label-controller /app/

CMD ["./node-label-controller"]