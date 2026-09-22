package legacyflate

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand/v2"
	"runtime"
	"testing"
)

func TestFrozenEncoder(t *testing.T) {
	for _, size := range []int{0, 1, 16, 17, 127, 128, 1024, 65534, 65535, 65536, 131070, 262144} {
		for _, pattern := range []string{"random", "repeat", "mixed"} {
			t.Run(fmt.Sprintf("%s/%d", pattern, size), func(t *testing.T) {
				input := make([]byte, size)
				rng := rand.New(rand.NewPCG(1, 2))
				for i := range input {
					switch pattern {
					case "random":
						input[i] = byte(rng.Uint32())
					case "repeat":
						input[i] = byte(i % 13)
					case "mixed":
						if i%200 < 100 {
							input[i] = byte(rng.Uint32())
						} else {
							input[i] = byte(i % 13)
						}
					}
				}
				got := Encode(input)
				r := flate.NewReader(bytes.NewReader(got))
				decoded, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(decoded, input) {
					t.Fatal("round trip changed input")
				}
				if runtime.Version() == "go1.26.6" {
					var expected bytes.Buffer
					w, err := flate.NewWriter(&expected, flate.BestSpeed)
					if err != nil {
						t.Fatal(err)
					}
					for at := 0; at < len(input); {
						n := min(len(input)-at, 777)
						if _, err := w.Write(input[at : at+n]); err != nil {
							t.Fatal(err)
						}
						at += n
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, expected.Bytes()) {
						t.Fatalf("frozen output differs: got %x want %x", sha256.Sum256(got), sha256.Sum256(expected.Bytes()))
					}
				}
			})
		}
	}
}
