package discovery

import "testing"

func TestQueryAcceptsOnlySupportedBilibiliInputs(t *testing.T) {
	tests := []struct{ input, kind, id string }{
		{" 12345 ", "creator", "12345"},
		{"https://space.bilibili.com/12345/video?spm_id_from=abc", "creator", "12345"},
		{"BV17x411w7KC", "video", "BV17x411w7KC"},
		{"https://www.bilibili.com/video/BV17x411w7KC/?p=1", "video", "BV17x411w7KC"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			kind, id, err := ParseQuery(test.input)
			if err != nil || kind != test.kind || id != test.id {
				t.Fatalf("got %s %s %v", kind, id, err)
			}
		})
	}
	for _, input := range []string{"", "0", "-2", "18446744073709551616", "https://space.bilibili.com.evil.example/12345", "https://user:secret@space.bilibili.com/12345", "javascript:alert(1)", "https://www.bilibili.com/video/BVinvalid", "http://localhost/123"} {
		if _, _, err := ParseQuery(input); err == nil {
			t.Errorf("accepted invalid input %q", input)
		}
	}
}
