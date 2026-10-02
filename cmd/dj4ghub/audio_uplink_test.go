package main

import (
	"encoding/binary"
	"math"
	"testing"
)

func waveFormat(tag uint16, channels, rate, blockAlign int, subformat uint32) []byte {
	raw := make([]byte, 40)
	binary.LittleEndian.PutUint16(raw[0:], tag)
	binary.LittleEndian.PutUint16(raw[2:], uint16(channels))
	binary.LittleEndian.PutUint32(raw[4:], uint32(rate))
	binary.LittleEndian.PutUint16(raw[12:], uint16(blockAlign))
	binary.LittleEndian.PutUint32(raw[24:], subformat)
	return raw
}

func TestParseWaveFormat(t *testing.T) {
	// The module render endpoint as reported by Windows: float, mono, 8 kHz.
	module, ok := parseWaveFormat(waveFormat(3, 1, 8000, 4, 0))
	if !ok || !module.Float || module.Channels != 1 || module.Rate != 8000 || module.BytesPerSample != 4 {
		t.Fatalf("module format = %+v %v", module, ok)
	}
	mic, ok := parseWaveFormat(waveFormat(0xFFFE, 2, 48000, 8, 3))
	if !ok || !mic.Float || mic.Channels != 2 || mic.Rate != 48000 {
		t.Fatalf("extensible float = %+v %v", mic, ok)
	}
	pcm, ok := parseWaveFormat(waveFormat(0xFFFE, 2, 44100, 4, 1))
	if !ok || pcm.Float || pcm.BytesPerSample != 2 {
		t.Fatalf("extensible pcm = %+v %v", pcm, ok)
	}
	if _, ok := parseWaveFormat(waveFormat(0x55, 2, 44100, 4, 0)); ok {
		t.Fatal("compressed format accepted")
	}
}

func TestDecodeMonoAndEncodeFrames(t *testing.T) {
	stereo := sampleFormat{Channels: 2, Rate: 48000, BytesPerSample: 4, Float: true}
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:], math.Float32bits(0.5))
	binary.LittleEndian.PutUint32(data[4:], math.Float32bits(-0.5))
	binary.LittleEndian.PutUint32(data[8:], math.Float32bits(0.2))
	binary.LittleEndian.PutUint32(data[12:], math.Float32bits(0.4))
	mono := decodeMono(stereo, data, 2, nil)
	if len(mono) != 2 || mono[0] != 0 || math.Abs(float64(mono[1]-0.3)) > 1e-6 {
		t.Fatalf("mono = %v", mono)
	}
	pcm16 := sampleFormat{Channels: 1, Rate: 8000, BytesPerSample: 2}
	out := make([]byte, 4)
	encodeFrames(pcm16, []float32{1.5, -0.5}, out)
	if got := int16(binary.LittleEndian.Uint16(out[0:])); got != 32767 {
		t.Fatalf("clipped sample = %d", got)
	}
	if got := readSample(pcm16, out[2:]); math.Abs(float64(got+0.5)) > 1e-3 {
		t.Fatalf("round trip = %v", got)
	}
}

func TestRateConverterDownsamplesTo8kHz(t *testing.T) {
	converter := &rateConverter{in: 48000, out: 8000}
	input := make([]float32, 4800) // 100 ms
	for i := range input {
		input[i] = 0.25
	}
	out := converter.convert(input, nil)
	if len(out) != 800 {
		t.Fatalf("output samples = %d, want 800", len(out))
	}
	for _, sample := range out {
		if math.Abs(float64(sample-0.25)) > 1e-6 {
			t.Fatalf("sample = %v", sample)
		}
	}
	odd := &rateConverter{in: 44100, out: 8000}
	if got := len(odd.convert(make([]float32, 44100), nil)); got != 8000 {
		t.Fatalf("44.1 kHz second -> %d samples", got)
	}
}

func TestNormalizeBrowserDeviceLabel(t *testing.T) {
	for label, want := range map[string]struct {
		name string
		role endpointRole
	}{
		"預設 - 麥克風 (2- AVerMedia AM310 USB Microphone)": {"麥克風 (2- AVerMedia AM310 USB Microphone)", roleDefault},
		"通訊 - 麥克風 (AC Interface) (2ca3:4006)":          {"麥克風 (AC Interface)", roleCommunications},
		"麥克風 (3- TUF GAMING H1 Wireless) (0b05:1a52)":  {"麥克風 (3- TUF GAMING H1 Wireless)", roleNamed},
		"Default - Microphone (Realtek(R) Audio)":      {"Microphone (Realtek(R) Audio)", roleDefault},
	} {
		name, role := normalizeBrowserDeviceLabel(label)
		if name != want.name || role != want.role {
			t.Fatalf("normalize(%q) = %q %v, want %q %v", label, name, role, want.name, want.role)
		}
	}
	if !isModuleAudioEndpoint("麥克風 (AC Interface)") || isModuleAudioEndpoint("麥克風 (2- AVerMedia AM310 USB Microphone)") {
		t.Fatal("module endpoint detection")
	}
}
