package renom

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	audioPacketData byte = 0x01
	audioPacketEnd  byte = 0x02
	maxAudioPayload      = 1199
)

var audioEndPacket = []byte{audioPacketEnd}

type packetAudioWriter struct {
	w io.Writer
}

func newPacketAudioWriter(w io.Writer) *packetAudioWriter {
	return &packetAudioWriter{w: w}
}

func (w *packetAudioWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > maxAudioPayload {
			n = maxAudioPayload
		}

		packet := make([]byte, n+1)
		packet[0] = audioPacketData
		copy(packet[1:], p[:n])

		if _, err := w.w.Write(packet); err != nil {
			return total, err
		}

		total += n
		p = p[n:]
	}

	return total, nil
}

func (w *packetAudioWriter) Close() error {
	var lastErr error
	for range 5 {
		if _, err := w.w.Write(audioEndPacket); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

type AudioStreamer struct {
	writer *packetAudioWriter
}

func NewAudioStreamer(w io.Writer) *AudioStreamer {
	return &AudioStreamer{writer: newPacketAudioWriter(w)}
}

func (as *AudioStreamer) StreamAudio() error {
	audioPath, err := findAudioFile("440hz.mp3")
	if err != nil {
		return err
	}

	log.Printf("Starting audio from %s", audioPath)
	cmd := exec.Command("gst-launch-1.0",
		"filesrc", "location="+audioPath, "!",
		"decodebin", "!",
		"audioconvert", "!",
		"audioresample", "!",
		"audio/x-raw,rate=44100,channels=2,format=S16LE,layout=interleaved", "!",
		"queue", "max-size-time=1000000000", "!",
		"fdsink", "fd=1", "sync=true")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gstreamer: %w", err)
	}

	logCommandStderr("GStreamer stderr", stderr)
	log.Printf("GStreamer audio pipeline started, streaming audio data...")

	buffer := make([]byte, 4096)
	totalBytesSent := int64(0)
	readCount := 0

	for {
		n, err := stdout.Read(buffer)
		readCount++
		if err != nil {
			if err == io.EOF {
				log.Printf("Audio stream completed. Total bytes sent: %d, read attempts: %d", totalBytesSent, readCount)
				break
			}
			return fmt.Errorf("failed to read from audio pipeline after %d reads: %w", readCount, err)
		}

		if n > 0 {
			written, err := as.writer.Write(buffer[:n])
			if err != nil {
				return fmt.Errorf("failed to write audio data after %d bytes: %w", totalBytesSent, err)
			}
			totalBytesSent += int64(written)

			if totalBytesSent%262144 == 0 {
				log.Printf("Sent %.1f MB of audio data", float64(totalBytesSent)/1048576)
			}
		}
	}

	if err := as.writer.Close(); err != nil {
		log.Printf("Error sending audio end marker: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("GStreamer process ended with error: %v", err)
	}

	log.Printf("Audio streaming completed successfully. Total bytes sent: %d", totalBytesSent)
	return nil
}

type AudioReceiver struct {
	reader io.Reader
}

func NewAudioReceiver(r io.Reader) *AudioReceiver {
	return &AudioReceiver{reader: r}
}

func (ar *AudioReceiver) ReceiveAudio() error {
	log.Printf("Starting real-time audio playback from stream")

	recorder, err := newWAVRecorder()
	if err != nil {
		return err
	}
	defer func() {
		if err := recorder.Close(); err != nil {
			log.Printf("failed to close WAV recording: %v", err)
		}
	}()

	args := []string{
		"fdsrc", "fd=0", "!",
		"rawaudioparse", "use-sink-caps=false", "sample-rate=44100", "num-channels=2", "format=pcm", "pcm-format=s16le", "!",
		"audioconvert", "!",
		"audioresample", "!",
		"queue", "max-size-time=50000000", "leaky=downstream", "!",
		"autoaudiosink", "sync=false",
	}

	cmd := exec.Command("gst-launch-1.0", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gstreamer playback: %w", err)
	}

	logCommandStderr("GStreamer stderr", stderr)
	log.Printf("GStreamer audio playback pipeline started")

	buffer := make([]byte, 1500)
	totalBytes := int64(0)

	for {
		n, err := ar.reader.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("Audio stream reception completed. Total bytes received: %d", totalBytes)
				break
			}
			return fmt.Errorf("failed to read from stream: %w", err)
		}
		if n == 0 {
			continue
		}

		switch buffer[0] {
		case audioPacketData:
			payload := buffer[1:n]
			if _, err := recorder.Write(payload); err != nil {
				return fmt.Errorf("failed to write WAV recording: %w", err)
			}

			written, err := stdin.Write(payload)
			if err != nil {
				return fmt.Errorf("failed to write to gstreamer: %w", err)
			}
			totalBytes += int64(written)

			if totalBytes%262144 == 0 {
				log.Printf("Received and playing %.1f MB of audio data", float64(totalBytes)/1048576)
			}
		case audioPacketEnd:
			log.Printf("Audio stream end marker received. Total bytes received: %d", totalBytes)
			_ = stdin.Close()
			if err := cmd.Wait(); err != nil {
				log.Printf("GStreamer playback process ended with error: %v", err)
			}
			log.Printf("Audio playback completed successfully. Total bytes received: %d", totalBytes)
			return nil
		default:
			log.Printf("Dropping unknown audio packet type=%d size=%d", buffer[0], n)
		}
	}

	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		log.Printf("GStreamer playback process ended with error: %v", err)
	}

	log.Printf("Audio playback completed successfully. Total bytes received: %d", totalBytes)
	return nil
}

func logCommandStderr(prefix string, stderr io.Reader) {
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				break
			}
			if n > 0 {
				log.Printf("%s: %s", prefix, string(bytes.TrimSpace(buf[:n])))
			}
		}
	}()
}

func findAudioFile(name string) (string, error) {
	candidates := []string{
		filepath.Join("static", name),
		filepath.Join("..", "static", name),
		filepath.Join("ice-signal-renom", "static", name),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}

	return "", fmt.Errorf("audio source %s not found; expected static/%s", name, name)
}
