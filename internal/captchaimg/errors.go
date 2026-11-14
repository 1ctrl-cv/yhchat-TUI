package captchaimg

import "errors"

// Sentinel errors returned (wrapped with %w so callers can use errors.Is) by
// DecodeDataURL. The TUI login screen treats any of them as "captcha image
// could not be shown" and falls back to a text message.
var (
	// ErrEmptyDataURL means the caller passed an empty/whitespace-only string.
	ErrEmptyDataURL = errors.New("captchaimg: empty data URL")

	// ErrInvalidDataURL means the string is not a base64 image data URL at all
	// (missing "data:" prefix or comma, no ";base64" marker, empty payload, ...).
	ErrInvalidDataURL = errors.New("captchaimg: invalid data URL")

	// ErrUnsupportedMediaType means the data URL points at a format this
	// package cannot decode (only image/png and image/jpeg are supported).
	ErrUnsupportedMediaType = errors.New("captchaimg: unsupported media type")

	// ErrInvalidBase64 means the payload after the comma is not valid base64.
	ErrInvalidBase64 = errors.New("captchaimg: invalid base64 payload")

	// ErrCorruptImage means the bytes decoded fine but are not a decodable image.
	ErrCorruptImage = errors.New("captchaimg: corrupt image data")

	// ErrImageTooLarge means the image header declares an implausibly large
	// canvas; decoding it could exhaust memory.
	ErrImageTooLarge = errors.New("captchaimg: image dimensions too large")
)
