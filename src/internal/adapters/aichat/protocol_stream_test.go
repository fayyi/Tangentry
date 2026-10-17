package aichat

import (
	"strings"
	"testing"
)

func TestParseStream(t *testing.T) {
	var got strings.Builder
	input := "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"，世界\"}}]}\n\n" + "data: [DONE]\n\n"
	if err := parseStream(strings.NewReader(input), func(value string) error { _, err := got.WriteString(value); return err }); err != nil {
		t.Fatal(err)
	}
	if got.String() != "你好，世界" {
		t.Fatalf("stream chunks = %q", got.String())
	}
}

func TestParseStreamRejectsEmpty(t *testing.T) {
	if err := parseStream(strings.NewReader("data: [DONE]\n\n"), func(string) error { return nil }); err == nil {
		t.Fatal("empty stream accepted")
	}
}
