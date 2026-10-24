package worker

import (
	"bufio"
	"bytes"
	"testing"
)

func TestJSONRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	input := Event{Type: "final", SessionID: "session", Sequence: 3, Start: 1.2, End: 2.4, Text: "你好"}
	if err := WriteJSON(&buffer, input); err != nil {
		t.Fatal(err)
	}
	output, err := ReadJSON(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatal(err)
	}
	if output.Version != ProtocolVersion || output.Type != input.Type || output.Text != input.Text {
		t.Fatalf("unexpected event: %#v", output)
	}
}

func TestPCMFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	input := []byte{0, 1, 2, 3}
	if err := WritePCM(&buffer, input); err != nil {
		t.Fatal(err)
	}
	_, pcm, err := ReadFrame(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pcm, input) {
		t.Fatalf("pcm = %v", pcm)
	}
}
