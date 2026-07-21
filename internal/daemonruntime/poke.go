package daemonruntime

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
)

const pokeBodyLimit = 10 * 1024

var ErrPokeBodyTooLarge = errors.New("poke body exceeds 10 KB")

func readPokeInput(r *http.Request) (awarenessdomain.PokeInput, error) {
	if r == nil || r.Body == nil {
		return awarenessdomain.PokeInput{}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, pokeBodyLimit+1))
	if err != nil {
		return awarenessdomain.PokeInput{}, err
	}
	if len(raw) == 0 {
		return awarenessdomain.PokeInput{}, nil
	}
	if len(raw) > pokeBodyLimit {
		return awarenessdomain.PokeInput{}, ErrPokeBodyTooLarge
	}
	input := awarenessdomain.PokeInput{
		ContentType: r.Header.Get("Content-Type"),
		HasBody:     true,
	}
	input = input.Normalize()
	if pokeBodyLooksTextual(input.ContentType, raw) {
		input.BodyText = strings.TrimSpace(string(bytes.ToValidUTF8(raw, []byte("?"))))
	}
	return input.Normalize(), nil
}

func pokeBodyLooksTextual(contentType string, raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/json":
		return true
	case strings.HasSuffix(contentType, "+json"):
		return true
	case contentType == "application/xml":
		return true
	case strings.HasSuffix(contentType, "+xml"):
		return true
	case contentType == "application/x-www-form-urlencoded":
		return true
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	return utf8.Valid(raw)
}
