package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// generateQRPNG renders a QR code as a PNG with a dark frame.
// Outer image: 220x220 dark background (#0c0c0c).
// Inner QR area: 180x180 white box centered, with 1px green (#34ed7a) border.
// QR modules: black on white (standard, scannable).
func generateQRPNG(data string) ([]byte, error) {
	modules := encodeQR(data)
	if modules == nil {
		// Fallback: 1x1 white pixel
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
		var buf bytes.Buffer
		png.Encode(&buf, img)
		return buf.Bytes(), nil
	}

	const (
		imgSize  = 220
		qrBoxW   = 180
		qrBoxH   = 180
		borderPx = 1
	)

	// Center the QR white box
	qrX := (imgSize - qrBoxW) / 2
	qrY := (imgSize - qrBoxH) / 2

	dark := color.NRGBA{0x0c, 0x0c, 0x0c, 255}
	white := color.NRGBA{255, 255, 255, 255}
	black := color.NRGBA{0, 0, 0, 255}
	green := color.NRGBA{0x34, 0xed, 0x7a, 255}

	img := image.NewNRGBA(image.Rect(0, 0, imgSize, imgSize))

	// Fill dark background
	for y := 0; y < imgSize; y++ {
		for x := 0; x < imgSize; x++ {
			img.SetNRGBA(x, y, dark)
		}
	}

	// Fill white QR box
	for y := qrY; y < qrY+qrBoxH; y++ {
		for x := qrX; x < qrX+qrBoxW; x++ {
			img.SetNRGBA(x, y, white)
		}
	}

	// Draw 1px green border around the white box
	for x := qrX - borderPx; x < qrX+qrBoxW+borderPx; x++ {
		if x >= 0 && x < imgSize {
			// Top border
			if qrY-borderPx >= 0 {
				img.SetNRGBA(x, qrY-borderPx, green)
			}
			// Bottom border
			if qrY+qrBoxH < imgSize {
				img.SetNRGBA(x, qrY+qrBoxH, green)
			}
		}
	}
	for y := qrY - borderPx; y < qrY+qrBoxH+borderPx; y++ {
		if y >= 0 && y < imgSize {
			// Left border
			if qrX-borderPx >= 0 {
				img.SetNRGBA(qrX-borderPx, y, green)
			}
			// Right border
			if qrX+qrBoxW < imgSize {
				img.SetNRGBA(qrX+qrBoxW, y, green)
			}
		}
	}

	// Render QR modules (black on white) within the white box
	n := len(modules)
	if n > 0 {
		// Leave a 4-module quiet zone within the white box
		margin := 4
		available := qrBoxW - margin*2
		cellSize := available / n
		if cellSize < 1 {
			cellSize = 1
		}
		offsetX := qrX + (qrBoxW-n*cellSize)/2
		offsetY := qrY + (qrBoxH-n*cellSize)/2

		for row := 0; row < n; row++ {
			for col := 0; col < n; col++ {
				if modules[row][col] {
					for dy := 0; dy < cellSize; dy++ {
						for dx := 0; dx < cellSize; dx++ {
							px := offsetX + col*cellSize + dx
							py := offsetY + row*cellSize + dy
							if px >= 0 && px < imgSize && py >= 0 && py < imgSize {
								img.SetNRGBA(px, py, black)
							}
						}
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
