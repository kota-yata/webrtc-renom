package renom

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

type wavRecorder struct {
	file      *os.File
	path      string
	dataBytes uint32
}

func newWAVRecorder() (*wavRecorder, error) {
	if err := os.MkdirAll("recordings", 0o755); err != nil {
		return nil, fmt.Errorf("create recordings directory: %w", err)
	}

	name := "received-" + time.Now().Format("20060102-150405") + ".wav"
	path := filepath.Join("recordings", name)
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create WAV recording: %w", err)
	}

	r := &wavRecorder{file: file, path: path}
	if err := r.writeHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}
	log.Printf("recording received audio to %s", path)
	return r, nil
}

func (r *wavRecorder) Write(p []byte) (int, error) {
	n, err := r.file.Write(p)
	r.dataBytes += uint32(n)
	return n, err
}

func (r *wavRecorder) Close() error {
	if _, err := r.file.Seek(0, io.SeekStart); err != nil {
		_ = r.file.Close()
		return err
	}
	if err := r.writeHeader(); err != nil {
		_ = r.file.Close()
		return err
	}
	if err := r.file.Close(); err != nil {
		return err
	}
	log.Printf("saved WAV recording to %s bytes=%d", r.path, r.dataBytes)
	return nil
}

func (r *wavRecorder) writeHeader() error {
	const (
		sampleRate    uint32 = 44100
		channels      uint16 = 2
		bitsPerSample uint16 = 16
		audioFormat   uint16 = 1
	)

	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	blockAlign := channels * bitsPerSample / 8

	if _, err := r.file.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, uint32(36)+r.dataBytes); err != nil {
		return err
	}
	if _, err := r.file.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, audioFormat); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, channels); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, sampleRate); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(r.file, binary.LittleEndian, bitsPerSample); err != nil {
		return err
	}
	if _, err := r.file.Write([]byte("data")); err != nil {
		return err
	}
	return binary.Write(r.file, binary.LittleEndian, r.dataBytes)
}
