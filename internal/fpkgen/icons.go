package fpkgen

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

//go:embed defaults/ICON.PNG defaults/ICON_256.PNG
var defaultIcons embed.FS

// handleIcons downloads/generates and saves all required icon files
func (g *Generator) handleIcons(appDir string, config *AppConfig) error {
	var originalIcon image.Image
	var err error

	// Try to load icon from various sources
	if config.Icon != "" {
		if strings.HasPrefix(config.Icon, "file://") {
			// Load from local file path
			localPath := strings.TrimPrefix(config.Icon, "file://")
			originalIcon, err = loadLocalIcon(localPath)
			if err != nil {
				fmt.Printf("Warning: Failed to load icon from %s: %v\n", localPath, err)
			}
		} else if strings.HasPrefix(config.Icon, "http") {
			// Download from URL
			originalIcon, err = downloadIcon(config.Icon)
			if err != nil {
				fmt.Printf("Warning: Failed to download icon from %s: %v\n", config.Icon, err)
			}
		}
	}

	// If no icon downloaded, use embedded default icon
	if originalIcon == nil {
		originalIcon, err = loadDefaultIcon()
		if err != nil {
			return fmt.Errorf("failed to load default icon: %w", err)
		}
	}

	// Resize to required sizes
	icon64 := resizeImage(originalIcon, 64, 64)
	icon256 := resizeImage(originalIcon, 256, 256)

	// Save to all required locations
	locations64 := []string{
		filepath.Join(appDir, "ICON.PNG"),
		filepath.Join(appDir, "app", "ui", "images", "icon_64.png"),
	}

	locations256 := []string{
		filepath.Join(appDir, "ICON_256.PNG"),
		filepath.Join(appDir, "app", "ui", "images", "icon_256.png"),
	}

	for _, path := range locations64 {
		if err := saveImage(icon64, path); err != nil {
			return fmt.Errorf("failed to save icon to %s: %w", path, err)
		}
	}

	for _, path := range locations256 {
		if err := saveImage(icon256, path); err != nil {
			return fmt.Errorf("failed to save icon to %s: %w", path, err)
		}
	}

	return nil
}

// loadDefaultIcon loads the embedded default icon
func loadDefaultIcon() (image.Image, error) {
	data, err := defaultIcons.ReadFile("defaults/ICON_256.PNG")
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	return img, nil
}

// loadLocalIcon loads an icon from local file path
func loadLocalIcon(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// downloadIcon downloads an icon from URL
func downloadIcon(url string) (image.Image, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download icon: status %d", resp.StatusCode)
	}

	// Read the body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Decode the image
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// resizeImage resizes an image to the specified dimensions
func resizeImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// saveImage saves an image to a file
func saveImage(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}
