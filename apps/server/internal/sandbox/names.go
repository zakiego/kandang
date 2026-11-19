package sandbox

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Name errors. The API turns them into 400s with the file's index.
var (
	ErrNameEmpty   = errors.New("name is empty")
	ErrNameDotDot  = errors.New("name contains ..")
	ErrNameControl = errors.New("name contains control characters")
	ErrNameLong    = errors.New("name is longer than 128 bytes")
	ErrNameUTF8    = errors.New("name isn't valid UTF-8")
)

// CleanName turns a caller's file name into a safe basename. Directories are
// dropped ("data/q1.csv" becomes "q1.csv"); a ".." anywhere, control
// characters, invalid UTF-8, an empty result or more than 128 bytes are
// rejected. Both slashes count as separators so Windows paths clean too.
func CleanName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrNameUTF8
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return "", ErrNameControl
		}
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	for _, p := range parts {
		if p == ".." {
			return "", ErrNameDotDot
		}
	}
	if len(parts) == 0 {
		return "", ErrNameEmpty
	}
	base := parts[len(parts)-1]
	if strings.TrimSpace(base) == "" || base == "." {
		return "", ErrNameEmpty
	}
	if len(base) > MaxNameBytes {
		return "", ErrNameLong
	}
	return base, nil
}
