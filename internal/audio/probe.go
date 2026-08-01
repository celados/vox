package audio

import (
	"encoding/binary"
	"errors"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// ErrUnknownDuration means neither the container nor ffprobe could answer. The
// caller decides what to do: for vox that is a warning plus a size-only check,
// because refusing every non-WAV file on machines without ffmpeg would be worse
// than occasionally letting the server reject an over-long upload.
var ErrUnknownDuration = errors.New("no duration: install ffmpeg for non-WAV input")

// Duration returns the audio length in seconds, rounded up so a file just over
// a limit is not rounded back under it.
func Duration(path string, data []byte) (int, error) {
	if sec, ok := wavDuration(data); ok {
		return sec, nil
	}
	if sec, ok := ffprobeDuration(path); ok {
		return sec, nil
	}
	return 0, ErrUnknownDuration
}

// wavDuration reads the fmt and data chunks of a RIFF/WAVE file.
func wavDuration(data []byte) (int, bool) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate uint32
	for pos := 12; pos+8 <= len(data); {
		id := string(data[pos : pos+4])
		size := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		body := pos + 8
		switch {
		case id == "fmt " && body+16 <= len(data):
			byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
		case id == "data" && byteRate > 0:
			return ceilDiv(int(size), int(byteRate)), true
		}
		pos = body + int(size)
		if size%2 == 1 {
			pos++ // RIFF chunks are word-aligned
		}
	}
	return 0, false
}

func ffprobeDuration(path string) (int, bool) {
	if path == "" || path == "mic" {
		return 0, false
	}
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0, false
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || sec <= 0 {
		return 0, false
	}
	return int(math.Ceil(sec)), true
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
