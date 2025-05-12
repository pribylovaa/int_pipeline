# Сборка приложения 
FROM golang:latest AS compiling_stage
RUN mkdir -p /go/src/int_pipeline
WORKDIR /go/scr/int_pipeline
ADD main.go .
ADD go.mod .
RUN go install .

# Минимальный образ с бинарником 
FROM alpine:latest
LABEL version="1.0.0"
WORKDIR /root/
COPY --from=compiling_stage /go/bin/int_pipeline .
ENTRYPOINT ./int_pipeline