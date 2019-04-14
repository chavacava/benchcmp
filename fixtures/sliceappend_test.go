package fixtures

// Borrowed from https://stackimpact.com/blog/practical-golang-benchmarks/
// by Dmitri Melikyan (https://github.com/dmelikyan)

import (
	"testing"
)

var numItems int = 1000000

func BenchmarkSliceAppend(b *testing.B) {
	s := make([]byte, 0)

	i := 0
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s = append(s, 1)

		i++
		if i == numItems {
			b.StopTimer()
			i = 0
			s = make([]byte, 0)
			b.StartTimer()
		}
	}
}

/*
func BenchmarkSliceAppend(b *testing.B) {
	s := make([]byte, 0, numItems)

	i := 0
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s = append(s, 1)

		i++
		if i == numItems {
			b.StopTimer()
			i = 0
			s = make([]byte, 0, numItems)
			b.StartTimer()
		}
	}
}
*/
