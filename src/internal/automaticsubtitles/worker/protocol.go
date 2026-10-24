// Package worker defines the small, versioned IPC contract used by a desktop
// streaming recognizer. Audio is PCM; control and recognition events are JSON.
package worker

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const ProtocolVersion = 1

type Event struct {
	Version   int     `json:"version"`
	Type      string  `json:"type"` // hello, ready, partial, final, error, stopped
	SessionID string  `json:"sessionId,omitempty"`
	Sequence  int64   `json:"sequence,omitempty"`
	Start     float64 `json:"start,omitempty"`
	End       float64 `json:"end,omitempty"`
	Text      string  `json:"text,omitempty"`
	Code      string  `json:"code,omitempty"`
	Message   string  `json:"message,omitempty"`
}

var ErrFrameTooLarge = errors.New("worker frame too large")

const maxFrameSize = 1 << 20
const pcmFrameFlag uint32 = 1 << 31

var ErrInvalidPCMFrame = errors.New("invalid PCM frame")

// WriteJSON writes a uint32 little-endian length followed by one JSON event.
func WriteJSON(w io.Writer, value Event) error {
	if value.Version == 0 {
		value.Version = ProtocolVersion
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) > maxFrameSize {
		return ErrFrameTooLarge
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func ReadJSON(r *bufio.Reader) (Event, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Event{}, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 || size&pcmFrameFlag != 0 || size > maxFrameSize {
		return Event{}, ErrFrameTooLarge
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Event{}, err
	}
	var value Event
	if err := json.Unmarshal(payload, &value); err != nil {
		return Event{}, fmt.Errorf("decode worker event: %w", err)
	}
	if value.Version != ProtocolVersion {
		return Event{}, fmt.Errorf("unsupported worker protocol version %d", value.Version)
	}
	return value, nil
}

// WritePCM writes raw little-endian PCM16 samples in the same length-framed
// stream as JSON events. The high bit distinguishes audio from JSON while
// retaining the existing JSON framing used by the first worker prototype.
func WritePCM(w io.Writer, pcm []byte) error {
	if len(pcm) == 0 || len(pcm) > int(pcmFrameFlag-1) {
		return ErrInvalidPCMFrame
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], pcmFrameFlag|uint32(len(pcm)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

// ReadFrame reads either a JSON worker event or a raw PCM frame. It is used by
// the desktop process manager; callers that only consume events can continue
// using ReadJSON for backwards compatibility.
func ReadFrame(r *bufio.Reader) (Event, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Event{}, nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 {
		return Event{}, nil, ErrFrameTooLarge
	}
	isPCM := size&pcmFrameFlag != 0
	if isPCM {
		size &^= pcmFrameFlag
		if size == 0 || size > pcmFrameFlag-1 {
			return Event{}, nil, ErrInvalidPCMFrame
		}
	} else if size > maxFrameSize {
		return Event{}, nil, ErrFrameTooLarge
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Event{}, nil, err
	}
	if isPCM {
		return Event{}, payload, nil
	}
	var value Event
	if err := json.Unmarshal(payload, &value); err != nil {
		return Event{}, nil, fmt.Errorf("decode worker event: %w", err)
	}
	if value.Version != ProtocolVersion {
		return Event{}, nil, fmt.Errorf("unsupported worker protocol version %d", value.Version)
	}
	return value, nil, nil
}
