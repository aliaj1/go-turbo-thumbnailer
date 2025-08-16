# Go Turbo Thumbnailer

A high-performance, concurrent-safe thumbnail generation library for Go.

This library leverages `libjpeg-turbo` via CGo to provide a significant performance boost for JPEG thumbnailing compared to Go's native `image/jpeg` package. It achieves this primarily through **decode-time scaling**, which is orders of magnitude faster than decoding a full-size image and then resizing it.

## Key Features

-   🚀 **Blazing Fast:** Utilizes `libjpeg-turbo` for decode-time scaling (1/2, 1/4, 1/8).
-   🧠 **Smart:** Optimized path for JPEG-to-JPEG conversion, with a graceful fallback to native Go for other formats (PNG, etc.).
-   💧 **Low Memory:** Streams from file readers and avoids loading entire files into memory.
-   ♻️ **GC-Friendly:** Uses `sync.Pool` to reuse pixel buffers, minimizing garbage collection overhead.
-   ⚡️ **Concurrent:** Includes a built-in, concurrent batch processor to utilize all CPU cores.
-   📦 **Generic API:** Works with file paths or `io.Reader`/`io.Writer` for maximum flexibility.

## Prerequisites

This library requires `libjpeg-turbo` to be installed on your system.

**macOS (using Homebrew):**
```bash
brew install jpeg-turbo
```

**Debian / Ubuntu:**
```bash
sudo apt-get update
sudo apt-get install libjpeg-turbo8-dev
```

**Fedora / CentOS:**
```bash
sudo dnf install libjpeg-turbo-devel
```

## Installation

```bash
go get github.com/aliaj1/go-turbo-thumbnailer
```

## Usage

### Simple Thumbnail Generation

```go
package main

import (
    "log"
    "github.com/aliaj1/go-turbo-thumbnailer"
)

func main() {
    opts := thumbnailer.Options{
        MaxWidth:  300,
        MaxHeight: 300,
        Quality:   85,
    }

    err := thumbnailer.Create("path/to/source.jpg", "path/to/thumb.jpg", opts)
    if err != nil {
        log.Fatalf("Failed to create thumbnail: %v", err)
    }

    log.Println("Thumbnail created successfully!")
}
```

### Concurrent Batch Processing

The library can process a large number of images concurrently, making full use of your CPU cores.

```go
jobs := map[string]string{
    "input/image1.jpg": "output/thumb1.jpg",
    "input/image2.jpg": "output/thumb2.jpg",
    "input/image3.png": "output/thumb3.jpg", // Also works with other formats!
    // ... add thousands more
}

opts := thumbnailer.Options{MaxWidth: 150, MaxHeight: 150}
thumbnailer.CreateBatch(jobs, opts)

log.Println("All thumbnails processed!")
```

## Benchmarks

Benchmarks are run against a large (6000x4000) JPEG image, creating a 300x300 thumbnail. The results speak for themselves.

-   **`TurboJPEG`**: Our optimized `libjpeg-turbo` path.
-   **`NativeGo_nfntResize`**: A common approach using `image/jpeg` and the popular `nfnt/resize` library.
-   **`NativeGo_CustomResize`**: Uses `image/jpeg` and our custom (fast) box resize.

**Run the benchmarks yourself:** `go test -bench=. -benchmem`

## Benchmarks

Benchmarks are run against a large (6000x4000) JPEG image, creating a 300x300 thumbnail. The results demonstrate the massive performance advantage of this library's approach.

-   **`TurboJPEG`**: Our optimized `libjpeg-turbo` path.
-   **`NativeGo_nfntResize`**: A common pure-Go approach using `image/jpeg` and the popular `nfnt/resize` library.
-   **`NativeGo_CustomResize`**: Uses `image/jpeg` and this library's fallback pure-Go resizer.

**Run the benchmarks yourself:** `go test -bench=. -benchmem`

#### Real-World Results (11th Gen Intel i5 @ 2.40GHz on Linux):

```
goos: linux
goarch: amd64
pkg: github.com/aliaj1/go-turbo-thumbnailer
cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz
BenchmarkThumbnailGeneration/TurboJPEG-8                      58          20335651 ns/op         2953538 B/op        467 allocs/op
BenchmarkThumbnailGeneration/NativeGo_nfntResize-8                     3         426627013 ns/op        112132549 B/op        72 allocs/op
BenchmarkThumbnailGeneration/NativeGo_CustomResize-8                   2         668116453 ns/op        132527408 B/op     63253 allocs/op
```

### Analysis:

-   🚀 **Speed:** The `TurboJPEG` method is **21 to 33 times faster** than the pure Go alternatives (20ms vs. 426-668ms). This is the difference between an interactive response time and a noticeable delay.

-   💧 **Memory Efficiency:** It allocates **38 to 45 times less memory** per operation (~2.9 MB vs. ~112-132 MB). By avoiding the need to decode the full-resolution image into RAM, it dramatically reduces memory spikes and GC pressure.

-   ♻️ **Allocation Overhead:** While `TurboJPEG` makes more small allocations than `nfnt/resize`, its total memory footprint is vastly smaller. The benchmark also reveals that our fallback custom resizer is inefficient, highlighting the value of using a mature library like `nfnt/resize` for the pure Go path when `libjpeg-turbo` cannot be used.

This demonstrates the massive real-world performance advantage of using **decode-time scaling** via `libjpeg-turbo`.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.