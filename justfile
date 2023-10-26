executable := "target/movie-cal"

build:
  go build -o {{executable}} main.go

test:
  go test

run:
  go run main.go
