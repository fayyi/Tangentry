package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateBilibiliNFOKeepsSingleAndMultiPageLayout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		videos []string
		want   []string
	}{
		{"single", []string{"Video [BV17x411w7KC].mp4"}, []string{"Video [BV17x411w7KC].nfo"}},
		{"multi", []string{"Season 1/Video - S01E01 [BV17x411w7KC].mp4", "Season 1/Video - S01E02 [BV17x411w7KC].mp4"}, []string{"tvshow.nfo", "Season 1/Video - S01E01 [BV17x411w7KC].nfo", "Season 1/Video - S01E02 [BV17x411w7KC].nfo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			for _, video := range tc.videos {
				path := filepath.Join(directory, filepath.FromSlash(strings.TrimSuffix(video, filepath.Ext(video))+".info.json"))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				index := "1"
				if strings.Contains(video, "S01E02") {
					index = "2"
				}
				if err := os.WriteFile(path, []byte(`{"title":"Fixture video","description":"Description","uploader":"Uploader","upload_date":"20261020","playlist_index":`+index+`,"tags":["tag"]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := generateBilibiliNFO(directory, "https://www.bilibili.com/video/BV17x411w7KC/", tc.videos); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.want {
				if info, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name))); err != nil || info.Size() == 0 {
					t.Fatalf("missing NFO %q: %v", name, err)
				}
			}
		})
	}
}

func TestGeneratedSidecarsRejectSymlinkTargets(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.nfo")
	video := "Video [BV17x411w7KC].mp4"
	info := filepath.Join(directory, "Video [BV17x411w7KC].info.json")
	if err := os.WriteFile(info, []byte(`{"title":"Fixture video"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "Video [BV17x411w7KC].nfo")); err != nil {
		t.Fatal(err)
	}
	if err := generateBilibiliNFO(directory, "https://www.bilibili.com/video/BV17x411w7KC/", []string{video}); err == nil {
		t.Fatal("NFO writer followed a symlink target")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("symlink target was written: %v", err)
	}

	xmlPath := filepath.Join(directory, "Video.danmaku.xml")
	if err := os.WriteFile(xmlPath, []byte(`<i><d p="1,1,25,16711680,0,0,0,0">fixture</d></i>`), 0600); err != nil {
		t.Fatal(err)
	}
	assOutside := filepath.Join(t.TempDir(), "outside.ass")
	if err := os.Symlink(assOutside, filepath.Join(directory, "Video.danmaku.ass")); err != nil {
		t.Fatal(err)
	}
	if _, err := convertDanmakuXML(directory); err == nil {
		t.Fatal("ASS writer followed a symlink target")
	}
	if _, err := os.Stat(assOutside); !os.IsNotExist(err) {
		t.Fatalf("symlink target was written: %v", err)
	}
}

func TestDanmakuASSMapsReverseScrollAndColors(t *testing.T) {
	data := []byte(`<i><d p="1,1,25,16711680,0,0,0,0">scroll</d><d p="2,6,25,16711680,0,0,0,0">reverse</d><d p="3,4,25,16711680,0,0,0,0">bottom</d><d p="4,5,25,16711680,0,0,0,0">top</d></i>`)
	output, err := danmakuASS(data, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, `\move(1940,1040,-`) || !strings.Contains(text, `&H0000FF&}reverse`) || !strings.Contains(text, `0:00:02.00,0:00:10.00,Float`) {
		t.Fatalf("reverse danmaku/color conversion mismatch:\n%s", text)
	}
	if !strings.Contains(text, `0:00:03.00,0:00:08.00,Bottom,,0,0,0,,{\c&H0000FF&}bottom`) || !strings.Contains(text, `0:00:04.00,0:00:09.00,Top,,0,0,0,,{\c&H0000FF&}top`) {
		t.Fatalf("fixed-position danmaku mapping mismatch:\n%s", text)
	}
}
