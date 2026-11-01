// Package photos prepares photos of assets (docs/data-model.md): it checks an
// upload, turns it upright as its camera recorded, scales it down, and makes
// a thumbnail. Every photo is decoded and encoded again, so what is stored
// keeps no metadata from the file it came from, such as where it was taken.
package photos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

const (
	// MaxBytes caps an uploaded photo file.
	MaxBytes = 15 << 20
	// MaxSide caps each side of an upload, and MaxPixels its area, bounding
	// the memory a decode takes.
	MaxSide   = 12000
	MaxPixels = 50_000_000
	// Largest is the longest side a stored photo keeps; Thumb a thumbnail's.
	Largest = 2048
	Thumb   = 320
)

// ErrUnsupported is returned for a file that is not a PNG or JPEG image, or
// is too large to decode.
var ErrUnsupported = errors.New("only PNG and JPEG photos are supported")

// Prepared is a photo ready to store.
type Prepared struct {
	ContentType   string
	Full, Thumb   []byte
	Width, Height int
}

// Prepare checks a photo's bytes and makes what is stored of it.
func Prepare(data []byte) (*Prepared, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, ErrUnsupported
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return nil, fmt.Errorf("%w: photos are at most %d pixels a side and %d megapixels", ErrUnsupported, MaxSide, MaxPixels/1_000_000)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: it could not be decoded", ErrUnsupported)
	}
	if format == "jpeg" {
		img = upright(img, orientation(data))
	}
	full := scale(img, Largest)
	p := &Prepared{ContentType: "image/" + format, Width: full.Bounds().Dx(), Height: full.Bounds().Dy()}
	if p.Full, err = encode(full, format); err != nil {
		return nil, err
	}
	if p.Thumb, err = encode(scale(full, Thumb), format); err != nil {
		return nil, err
	}
	return p, nil
}

func encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	}
	return buf.Bytes(), err
}

// scale shrinks an image so its longer side is at most longest, keeping its
// shape; a smaller image is copied as it is.
func scale(img image.Image, longest int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > longest || h > longest {
		if w >= h {
			w, h = longest, max(1, h*longest/w)
		} else {
			w, h = max(1, w*longest/h), longest
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// upright turns an image as EXIF orientation o says it should be shown: 2-8
// are the mirrorings and quarter turns; anything else leaves it alone.
func upright(img image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if o >= 5 {
		w, h = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = b.Dx()-1-x, y
			case 3:
				dx, dy = b.Dx()-1-x, b.Dy()-1-y
			case 4:
				dx, dy = x, b.Dy()-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = b.Dy()-1-y, x
			case 7:
				dx, dy = b.Dy()-1-y, b.Dx()-1-x
			case 8:
				dx, dy = y, b.Dx()-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// orientation reads the EXIF orientation of a JPEG, or 1 when it has none
// or it cannot be read.
func orientation(data []byte) int {
	// Markers follow the start of image; APP1 holds EXIF.
	for i := 2; i+4 <= len(data) && data[i] == 0xFF; {
		marker, size := data[i+1], int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(data) {
			break
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	n := int(order.Uint16(t[ifd:]))
	for e := 0; e < n; e++ {
		at := ifd + 2 + 12*e
		if at+12 > len(t) {
			break
		}
		if order.Uint16(t[at:]) == 0x0112 {
			return int(order.Uint16(t[at+8:]))
		}
	}
	return 1
}
