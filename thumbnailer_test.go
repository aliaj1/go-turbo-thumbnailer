// thumbnailer_test.go

package thumbnailer

import (
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"testing"

	nfntResize "github.com/nfnt/resize"
)

const (
	testDir      = "testdata"
	largeImgPath = "testdata/large.jpg"
	benchWidth   = 300
	benchHeight  = 300
)

// setup creates a large dummy JPEG image for benchmarking if it doesn't exist.
func setup(tb testing.TB) {
	if _, err := os.Stat(largeImgPath); err == nil {
		return // Already exists
	}
	tb.Log("Creating large test image...")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		tb.Fatalf("Failed to create testdata dir: %v", err)
	}

	width, height := 6000, 4000
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Draw something so the JPEG is not trivial to compress
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 0, 255})
		}
	}

	f, err := os.Create(largeImgPath)
	if err != nil {
		tb.Fatalf("Failed to create test image file: %v", err)
	}
	defer f.Close()

	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		tb.Fatalf("Failed to encode test image: %v", err)
	}
	tb.Log("Test image created at", largeImgPath)
}

func BenchmarkThumbnailGeneration(b *testing.B) {
	setup(b)

	opts := Options{
		MaxWidth:  benchWidth,
		MaxHeight: benchHeight,
		Quality:   85,
	}

	b.Run("TurboJPEG", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			in, err := os.Open(largeImgPath)
			if err != nil {
				b.Fatal(err)
			}
			err = Process(in, io.Discard, opts)
			in.Close() // Close file manually as Process doesn't
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("NativeGo_nfntResize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			in, err := os.Open(largeImgPath)
			if err != nil {
				b.Fatal(err)
			}
			img, _, err := image.Decode(in) // Use image.Decode for fair comparison
			in.Close()
			if err != nil {
				b.Fatal(err)
			}
			thumb := nfntResize.Thumbnail(benchWidth, benchHeight, img, nfntResize.Lanczos3)
			err = jpeg.Encode(io.Discard, thumb, &jpeg.Options{Quality: 85})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("NativeGo_CustomResize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			in, err := os.Open(largeImgPath)
			if err != nil {
				b.Fatal(err)
			}
			err = createNativeThumbnail(in, io.Discard, opts)
			in.Close()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// To clean up after tests if desired
func TestMain(m *testing.M) {
	// Not setting up here to avoid setup cost in non-benchmark tests
	code := m.Run()
	// os.RemoveAll(testDir)
	os.Exit(code)
}
