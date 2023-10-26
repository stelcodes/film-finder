exec := "./target/movie-cal"
test-exec := "./local/movie-cal.test"

build:
  go build -o {{exec}} main.go

test:
  go test -o {{test-exec}}

run:
  go run main.go

test-with-benchmarks:
  go test -o {{test-exec}} -cpuprofile ./local/cpu.prof -benchmem -bench .

profile: test-with-benchmarks
  pprof -http=:7776 ./local/cpu.prof
