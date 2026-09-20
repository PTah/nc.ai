// Package chime carries the short end-of-run sound.
//
// The clip is embedded into the binary, so the same build works on macOS and
// Windows without shipping extra files next to the executable.
package chime

import (
	_ "embed"
	"encoding/base64"
)

//go:embed melodic_chime.wav
var clip []byte

// MimeType is the media type of the embedded clip.
const MimeType = "audio/wav"

// DataURL returns the clip as a data URL, ready for an <audio>/Audio() element
// in the webview. It is computed once: the clip never changes at runtime.
func DataURL() string {
	return "data:" + MimeType + ";base64," + base64.StdEncoding.EncodeToString(clip)
}

// Bytes returns a copy of the clip for callers that need the raw PCM/WAV data.
func Bytes() []byte {
	out := make([]byte, len(clip))
	copy(out, clip)
	return out
}
