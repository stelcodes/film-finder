package main

import (
	"testing"
)

func Benchmark_getClintonStateTheaterScreenings(b *testing.B) {
	for i := 0; i < b.N; i++ {
		scrapeClintonStateTheater()
	}
}
