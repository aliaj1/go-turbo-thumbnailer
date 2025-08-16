// Package thumbnailer provides a high-performance, concurrent-safe library
// for generating image thumbnails in Go. It leverages libjpeg-turbo for
// significant speed improvements over the native Go image/jpeg library.
package thumbnailer

import (
	_ "errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // Register PNG decoder
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"

	// CGo wrapper for the libjpeg-turbo library
	libjpeg "github.com/pixiv/go-libjpeg/jpeg"
)

// Options defines the parameters for thumbnail generation.
type Options struct {
	MaxWidth  int
	MaxHeight int
	Quality   int // JPEG quality (1-100)
}

// bufferPool holds reusable buffers for image pixel data, reducing GC pressure.
var bufferPool = sync.Pool{
	New: func() any {
		// 512KB can hold a 360x360 RGBA image.
		b := make([]byte, 512*1024)
		return &b
	},
}

// Create generates a single thumbnail from an input path to an output path.
// It automatically detects the input format and uses the highly optimized
// JPEG path when possible. The output is always a JPEG.
func Create(inputPath, outputPath string, opts Options) error {
	in, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	return Process(in, out, opts)
}

// CreateBatch generates thumbnails for a map of input/output paths concurrently.
// It uses a pool of workers equal to the number of CPU cores for optimal throughput.
func CreateBatch(jobs map[string]string, opts Options) {
	numWorkers := runtime.NumCPU()
	jobsCh := make(chan [2]string, len(jobs))
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsCh {
				if err := Create(job[0], job[1], opts); err != nil {
					// In a real app, you might want a more robust error handling mechanism
					fmt.Fprintf(os.Stderr, "ERROR: Failed to process %s: %v\n", job[0], err)
				}
			}
		}()
	}

	// Send jobs to workers
	for in, out := range jobs {
		jobsCh <- [2]string{in, out}
	}
	close(jobsCh)

	// Wait for all workers to finish
	wg.Wait()
}

// Process generates a thumbnail from an io.ReadSeeker to an io.Writer.
// This is the core function that allows for streaming and in-memory processing.
// The output is always a JPEG.
func Process(in io.ReadSeeker, out io.Writer, opts Options) error {
	// Sniff the first few bytes to check if it's a JPEG.
	header := make([]byte, 2)
	if _, err := io.ReadFull(in, header); err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}

	isJPEG := header[0] == 0xFF && header[1] == 0xD8

	if isJPEG {
		return createJPEGThumbnail(in, out, opts)
	}
	return createNativeThumbnail(in, out, opts)
}

// createJPEGThumbnail is the core of the high-performance JPEG-to-JPEG path.
func createJPEGThumbnail(in io.ReadSeeker, out io.Writer, opts Options) (err error) {
	cfg, err := libjpeg.DecodeConfig(in)
	if err != nil {
		return err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}

	wRatio := float64(cfg.Width) / float64(opts.MaxWidth)
	hRatio := float64(cfg.Height) / float64(opts.MaxHeight)
	ratio := math.Max(wRatio, hRatio)

	scaleDenom := 1
	if ratio > 8 {
		scaleDenom = 8
	} else if ratio > 4 {
		scaleDenom = 4
	} else if ratio > 2 {
		scaleDenom = 2
	}

	targetWidth := cfg.Width / scaleDenom
	targetHeight := cfg.Height / scaleDenom
	decodeOpts := &libjpeg.DecoderOptions{
		ScaleTarget: image.Rect(0, 0, targetWidth, targetHeight),
	}

	scaledImg, err := libjpeg.Decode(in, decodeOpts)
	if err != nil {
		if strings.Contains(err.Error(), "suspension") {
			// Log this as a warning, but continue processing the partial image
		} else {
			return err
		}
	}

	var srcImg *image.RGBA
	if rgba, ok := scaledImg.(*image.RGBA); ok {
		srcImg = rgba
	} else {
		b := scaledImg.Bounds()
		srcImg = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(srcImg, srcImg.Bounds(), scaledImg, b.Min, draw.Src)
	}

	var finalImg image.Image = srcImg
	scaledW := srcImg.Bounds().Dx()
	scaledH := srcImg.Bounds().Dy()

	if scaledW > opts.MaxWidth || scaledH > opts.MaxHeight {
		finalRatio := math.Min(float64(opts.MaxWidth)/float64(scaledW), float64(opts.MaxHeight)/float64(scaledH))
		finalW := int(math.Max(1.0, float64(scaledW)*finalRatio))
		finalH := int(math.Max(1.0, float64(scaledH)*finalRatio))

		resizeBufPtr, resizeDst := getBuffer(finalW, finalH)
		defer bufferPool.Put(resizeBufPtr)

		resizeRGBA(srcImg, resizeDst)
		finalImg = resizeDst
	}

	quality := opts.Quality
	if quality == 0 {
		quality = 85
	}
	return libjpeg.Encode(out, finalImg, &libjpeg.EncoderOptions{Quality: quality})
}

// createNativeThumbnail handles non-JPEG files using the standard Go libraries.
func createNativeThumbnail(in io.Reader, out io.Writer, opts Options) error {
	img, _, err := image.Decode(in)
	if err != nil {
		return err
	}

	resizedImg := resize(img, opts.MaxWidth, opts.MaxHeight)

	quality := opts.Quality
	if quality == 0 {
		quality = 85
	}
	return jpeg.Encode(out, resizedImg, &jpeg.Options{Quality: quality})
}

// getBuffer retrieves a sized buffer from the pool.
func getBuffer(width, height int) (*[]byte, *image.RGBA) {
	bufPtr := bufferPool.Get().(*[]byte)
	requiredSize := width * height * 4
	if cap(*bufPtr) < requiredSize {
		*bufPtr = make([]byte, requiredSize)
	}
	pix := (*bufPtr)[:requiredSize]
	img := &image.RGBA{
		Pix:    pix,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}
	return bufPtr, img
}

// resize performs a simple, fast downscale of any image.Image.
func resize(img image.Image, maxWidth, maxHeight int) image.Image {
	src := convertToNRGBA(img)
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	if srcW <= maxWidth && srcH <= maxHeight {
		return src
	}

	ratio := math.Min(float64(maxWidth)/float64(srcW), float64(maxHeight)/float64(srcH))
	dstW, dstH := int(math.Max(1.0, float64(srcW)*ratio)), int(math.Max(1.0, float64(srcH)*ratio))

	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	resizeNRGBA(src, dst)
	return dst
}

// convertToNRGBA ensures an image is in the NRGBA format.
func convertToNRGBA(img image.Image) *image.NRGBA {
	if nrgba, ok := img.(*image.NRGBA); ok && nrgba.Bounds().Min.Eq(image.Point{}) {
		return nrgba
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// resizeRGBA is a high-speed, simple box-resampling for RGBA images.
func resizeRGBA(src, dst *image.RGBA) {
	dstW, dstH := dst.Bounds().Dx(), dst.Bounds().Dy()
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	if dstW == 0 || dstH == 0 || srcW == 0 || srcH == 0 {
		return
	}
	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)

	for dy := 0; dy < dstH; dy++ {
		syStart := int(float64(dy) * yRatio)
		syEnd := int(float64(dy+1) * yRatio)
		dstOffset := dy * dst.Stride

		for dx := 0; dx < dstW; dx++ {
			sxStart := int(float64(dx) * xRatio)
			sxEnd := int(float64(dx+1) * xRatio)

			var r, g, b, a uint32
			var count uint32

			for sy := syStart; sy < syEnd; sy++ {
				srcRowOffset := sy * src.Stride
				for sx := sxStart; sx < sxEnd; sx++ {
					pixOffset := srcRowOffset + sx*4
					r += uint32(src.Pix[pixOffset])
					g += uint32(src.Pix[pixOffset+1])
					b += uint32(src.Pix[pixOffset+2])
					a += uint32(src.Pix[pixOffset+3])
					count++
				}
			}

			if count > 0 {
				dst.Pix[dstOffset] = uint8(r / count)
				dst.Pix[dstOffset+1] = uint8(g / count)
				dst.Pix[dstOffset+2] = uint8(b / count)
				dst.Pix[dstOffset+3] = uint8(a / count)
			}
			dstOffset += 4
		}
	}
}

// resizeNRGBA is the equivalent for NRGBA images (used in native path).
func resizeNRGBA(src, dst *image.NRGBA) {
	dstW, dstH := dst.Bounds().Dx(), dst.Bounds().Dy()
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)
	for dy := 0; dy < dstH; dy++ {
		syStart := int(float64(dy) * yRatio)
		syEnd := int(float64(dy+1) * yRatio)
		dstOffset := dy * dst.Stride
		for dx := 0; dx < dstW; dx++ {
			sxStart := int(float64(dx) * xRatio)
			sxEnd := int(float64(dx+1) * xRatio)
			var r, g, b, a uint32
			var count uint32 = 0
			for sy := syStart; sy < syEnd; sy++ {
				srcRowOffset := sy * src.Stride
				for sx := sxStart; sx < sxEnd; sx++ {
					pixOffset := srcRowOffset + sx*4
					r += uint32(src.Pix[pixOffset])
					g += uint32(src.Pix[pixOffset+1])
					b += uint32(src.Pix[pixOffset+2])
					a += uint32(src.Pix[pixOffset+3])
					count++
				}
			}
			if count > 0 {
				dst.Pix[dstOffset] = uint8(r / count)
				dst.Pix[dstOffset+1] = uint8(g / count)
				dst.Pix[dstOffset+2] = uint8(b / count)
				dst.Pix[dstOffset+3] = uint8(a / count)
			}
			dstOffset += 4
		}
	}
}
