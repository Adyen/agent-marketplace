package cache

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

func BenchmarkMutableLocalGitBatchDiskIONoNetwork(b *testing.B) {
	for _, warm := range []bool{false, true} {
		for _, count := range []int{1, 4} {
			b.Run(fmt.Sprintf("warm=%t/registries=%d", warm, count), func(b *testing.B) {
				isolateMutableGit(b)
				urls := make([]string, count)
				for i := range urls {
					urls[i] = mutableLocalRegistry(b)
					if _, err := CloneOrPull(urls[i], "main"); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if !warm {
						b.StopTimer()
						for _, url := range urls {
							if err := os.RemoveAll(RegistryCachePath(url, "main")); err != nil {
								b.Fatal(err)
							}
						}
						b.StartTimer()
					}
					var wg sync.WaitGroup
					for _, url := range urls {
						wg.Go(func() {
							if _, err := CloneOrPull(url, "main"); err != nil {
								b.Error(err)
							}
						})
					}
					wg.Wait()
				}
			})
		}
	}
}
