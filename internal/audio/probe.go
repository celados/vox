package audio

import (
	"encoding/binary"
	"os/exec"
	"strconv"
	"strings"
)

// Duration returns the audio length in seconds, or 0 when it cannot be
// determined. WAV is parsed inline; everything else defers to ffprobe, which is
// already a soft dependency for cache compression. An unknown duration is not
// fatal — the byte-size check still guards the request.
func Duration(path string, data []byte) int {
	if sec := wavDuration(data); sec > 0 {
		return sec
	}
	return ffprobeDuration(path)
}

// wavDuration reads the fmt and data chunks of a RIFF/WAVE file.
func wavDuration(data []byte) int {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0
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
			return int(size / byteRate)
		}
		pos = body + int(size)
		if size%2 == 1 {
			pos++ // RIFF chunks are word-aligned
		}
	}
	return 0
}

func ffprobeDuration(path string) int {
	if path == "" {
		return 0
	}
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return int(sec)
}
