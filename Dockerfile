FROM golang:1.16

WORKDIR /opt
COPY . .

RUN go build -o "app"

CMD ["/opt/app"]