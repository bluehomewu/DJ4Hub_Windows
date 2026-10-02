package main

import (
	"encoding/binary"
	"math"
	"regexp"
	"strings"
)

// sampleFormat describes the shared-mode mix format of a WASAPI endpoint.
type sampleFormat struct {
	Channels       int
	Rate           int
	BytesPerSample int
	Float          bool
}

var (
	chromeDeviceSuffix = regexp.MustCompile(`\s*\([0-9a-fA-F]{4}:[0-9a-fA-F]{4}\)\s*$`)
	chromeDefaultLabel = regexp.MustCompile(`^(?:預設|默认|預設值|Default)\s*-\s*`)
	chromeCommsLabel   = regexp.MustCompile(`^(?:通訊|通讯|Communications)\s*-\s*`)
)

// endpointRole is how a browser device label maps to a Windows endpoint.
type endpointRole int

const (
	roleNamed endpointRole = iota
	roleDefault
	roleCommunications
)

// normalizeBrowserDeviceLabel turns a Chrome label such as
// "預設 - 麥克風 (2- AVerMedia AM310) (07ca:3110)" into the Windows endpoint
// friendly name, and reports whether it named the default or the
// communications alias rather than a specific device.
func normalizeBrowserDeviceLabel(label string) (string, endpointRole) {
	label = strings.TrimSpace(label)
	role := roleNamed
	if chromeDefaultLabel.MatchString(label) {
		label, role = chromeDefaultLabel.ReplaceAllString(label, ""), roleDefault
	} else if chromeCommsLabel.MatchString(label) {
		label, role = chromeCommsLabel.ReplaceAllString(label, ""), roleCommunications
	}
	return strings.TrimSpace(chromeDeviceSuffix.ReplaceAllString(label, "")), role
}

func isModuleAudioEndpoint(name string) bool {
	return strings.Contains(name, "AC Interface") || strings.Contains(name, "AS Interface")
}

// parseWaveFormat reads a WAVEFORMATEX or WAVEFORMATEXTENSIBLE structure.
func parseWaveFormat(raw []byte) (sampleFormat, bool) {
	if len(raw) < 16 {
		return sampleFormat{}, false
	}
	tag := binary.LittleEndian.Uint16(raw[0:])
	format := sampleFormat{
		Channels: int(binary.LittleEndian.Uint16(raw[2:])),
		Rate:     int(binary.LittleEndian.Uint32(raw[4:])),
	}
	blockAlign := int(binary.LittleEndian.Uint16(raw[12:]))
	if format.Channels <= 0 || format.Rate <= 0 || blockAlign%format.Channels != 0 {
		return sampleFormat{}, false
	}
	format.BytesPerSample = blockAlign / format.Channels
	switch tag {
	case 1: // WAVE_FORMAT_PCM
	case 3: // WAVE_FORMAT_IEEE_FLOAT
		format.Float = true
	case 0xFFFE: // WAVE_FORMAT_EXTENSIBLE: the subformat GUID starts at byte 24.
		if len(raw) < 28 {
			return sampleFormat{}, false
		}
		switch binary.LittleEndian.Uint32(raw[24:]) {
		case 1:
		case 3:
			format.Float = true
		default:
			return sampleFormat{}, false
		}
	default:
		return sampleFormat{}, false
	}
	if format.Float && format.BytesPerSample != 4 {
		return sampleFormat{}, false
	}
	if !format.Float && format.BytesPerSample != 2 && format.BytesPerSample != 3 && format.BytesPerSample != 4 {
		return sampleFormat{}, false
	}
	return format, true
}

// decodeMono averages all channels of interleaved frames into mono samples.
func decodeMono(format sampleFormat, data []byte, frames int, dst []float32) []float32 {
	stride := format.Channels * format.BytesPerSample
	for frame := 0; frame < frames && (frame+1)*stride <= len(data); frame++ {
		var sum float32
		for channel := 0; channel < format.Channels; channel++ {
			sum += readSample(format, data[frame*stride+channel*format.BytesPerSample:])
		}
		dst = append(dst, sum/float32(format.Channels))
	}
	return dst
}

func readSample(format sampleFormat, b []byte) float32 {
	if format.Float {
		return math.Float32frombits(binary.LittleEndian.Uint32(b))
	}
	switch format.BytesPerSample {
	case 2:
		return float32(int16(binary.LittleEndian.Uint16(b))) / 32768
	case 3:
		v := int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24) >> 8
		return float32(v) / 8388608
	default:
		return float32(int32(binary.LittleEndian.Uint32(b))) / 2147483648
	}
}

// encodeFrames writes mono samples to every channel of the render format.
func encodeFrames(format sampleFormat, samples []float32, dst []byte) {
	stride := format.Channels * format.BytesPerSample
	for i, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		for channel := 0; channel < format.Channels; channel++ {
			b := dst[i*stride+channel*format.BytesPerSample:]
			switch {
			case format.Float:
				binary.LittleEndian.PutUint32(b, math.Float32bits(sample))
			case format.BytesPerSample == 2:
				binary.LittleEndian.PutUint16(b, uint16(int16(sample*32767)))
			case format.BytesPerSample == 3:
				v := int32(sample * 8388607)
				b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
			default:
				binary.LittleEndian.PutUint32(b, uint32(int32(sample*2147483647)))
			}
		}
	}
}

// rateConverter resamples mono audio. Downsampling averages the input
// samples that fall into each output period, which also acts as a simple
// anti-alias filter for voice; upsampling repeats the latest sample.
type rateConverter struct {
	in, out int
	pos     int
	sum     float32
	count   int
	last    float32
}

func (c *rateConverter) convert(input []float32, dst []float32) []float32 {
	if c.in == c.out {
		return append(dst, input...)
	}
	for _, sample := range input {
		c.sum += sample
		c.count++
		c.last = sample
		c.pos += c.out
		for c.pos >= c.in {
			c.pos -= c.in
			if c.count > 0 {
				dst = append(dst, c.sum/float32(c.count))
				c.sum, c.count = 0, 0
			} else {
				dst = append(dst, c.last)
			}
		}
	}
	return dst
}
