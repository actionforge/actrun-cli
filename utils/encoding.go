package utils

import (
	"bytes"
	"io"

	"golang.org/x/net/html/charset"
)

func DecodeBytes(data []byte) (string, error) {
	encoding, _, _ := charset.DetermineEncoding(data, "")
	if encoding != nil {
		decoder := encoding.NewDecoder()

		decodedData, err := io.ReadAll(decoder.Reader(bytes.NewReader(data)))
		if err != nil {
			return "", err
		}

		return string(decodedData), nil
	} else {
		return string(data), nil
	}
}
