package media

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
)

var ErrInvalid = errors.New("invalid media input")
var ErrTooLarge = errors.New("media too large")
var ErrUnsupported = errors.New("unsupported media type")
var ErrNotFound = errors.New("media not found")
var ErrConflict = errors.New("media conflict")
var ErrNotReady = errors.New("media not ready")
var ErrForbidden = errors.New("media forbidden")

type File struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"original_name"`
	MIME         string    `json:"mime_type"`
	Size         int64     `json:"byte_size"`
	SHA256       string    `json:"sha256"`
	Width        *int      `json:"width,omitempty"`
	Height       *int      `json:"height,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	OperationID  string    `json:"-"`
	Key          string    `json:"-"`
	Digest       [32]byte  `json:"-"`
}

func validateName(raw string) (string, string, error) {
	if !utf8.ValidString(raw) || len(raw) == 0 || len(raw) > 255 || raw != filepath.Base(raw) || strings.ContainsAny(raw, "/\\") || strings.HasPrefix(raw, ".") {
		return "", "", ErrInvalid
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			return "", "", ErrInvalid
		}
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(raw), "."))
	if ext == "" || len(ext) > 12 {
		return "", "", ErrUnsupported
	}
	lower := strings.ToLower(raw)
	for _, bad := range []string{".html.", ".htm.", ".svg.", ".js.", ".exe.", ".sh.", ".php."} {
		if strings.Contains(lower, bad) {
			return "", "", ErrUnsupported
		}
	}
	return raw, ext, nil
}

func inspect(s Storage, op, name string, size int64, head []byte) (string, *int, *int, error) {
	_, ext, err := validateName(name)
	if err != nil {
		return "", nil, nil, err
	}
	var expected string
	switch ext {
	case "png":
		expected = "image/png"
	case "jpg", "jpeg":
		expected = "image/jpeg"
	case "webp":
		expected = "image/webp"
	case "pdf":
		expected = "application/pdf"
	case "txt":
		expected = "text/plain"
	default:
		return "", nil, nil, ErrUnsupported
	}
	if strings.HasPrefix(expected, "image/") && size > MaxImageSize {
		return "", nil, nil, ErrTooLarge
	}
	if expected == "application/pdf" {
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return "", nil, nil, ErrUnsupported
		}
		reader, ok := s.(interface {
			OpenStage(string) (io.ReadCloser, error)
		})
		if !ok {
			return "", nil, nil, ErrInvalid
		}
		file, e := reader.OpenStage(op)
		if e != nil {
			return "", nil, nil, e
		}
		defer file.Close()
		var tail []byte
		buf := make([]byte, 32<<10)
		for {
			n, e := file.Read(buf)
			if n > 0 {
				tail = append(tail, buf[:n]...)
				if len(tail) > 1024 {
					tail = append([]byte(nil), tail[len(tail)-1024:]...)
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", nil, nil, e
			}
		}
		if !bytes.Contains(tail, []byte("%%EOF")) {
			return "", nil, nil, ErrUnsupported
		}
		return expected, nil, nil, nil
	}
	if expected == "text/plain" {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(head))), "<html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(head))), "<!doctype html") {
			return "", nil, nil, ErrUnsupported
		}
		reader, ok := s.(interface {
			OpenStage(string) (io.ReadCloser, error)
		})
		if !ok {
			return "", nil, nil, ErrInvalid
		}
		f, e := reader.OpenStage(op)
		if e != nil {
			return "", nil, nil, e
		}
		defer f.Close()
		br := bufio.NewReader(f)
		for {
			r, _, e := br.ReadRune()
			if e == io.EOF {
				break
			}
			if e != nil || r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') {
				return "", nil, nil, ErrUnsupported
			}
		}
		return expected, nil, nil, nil
	}
	if http.DetectContentType(head) != expected {
		return "", nil, nil, ErrUnsupported
	}
	reader, ok := s.(interface {
		OpenStage(string) (io.ReadCloser, error)
	})
	if !ok {
		return "", nil, nil, ErrInvalid
	}
	f, err := reader.OpenStage(op)
	if err != nil {
		return "", nil, nil, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(io.LimitReader(f, MaxImageSize))
	if err != nil || format != strings.TrimPrefix(expected, "image/") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 10000 || cfg.Height > 10000 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return "", nil, nil, ErrUnsupported
	}
	full, e := reader.OpenStage(op)
	if e != nil {
		return "", nil, nil, e
	}
	defer full.Close()
	decoded, actual, e := image.Decode(io.LimitReader(full, MaxImageSize))
	if e != nil || actual != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return "", nil, nil, ErrUnsupported
	}
	return expected, &cfg.Width, &cfg.Height, nil
}

func contentDisposition(name string, inline bool) string {
	kind := "attachment"
	if inline {
		kind = "inline"
	}
	return mime.FormatMediaType(kind, map[string]string{"filename": name})
}

func digestHex(d [32]byte) string { return hex.EncodeToString(d[:]) }
