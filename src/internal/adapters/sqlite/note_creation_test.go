package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/notes"
)

func TestPairedCreationRollsBackImageAndReceiptFailures(t *testing.T) {
	for _, table := range []string{"note_frames", "note_creations"} {
		t.Run(table, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "notes.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := context.Background()
			position := 2.0
			note := notes.Note{
				ID: strings.Repeat("a", 32), CreationHash: "test-fingerprint",
				ContentID: "youtube:abcdefghijk", Body: "正文", PositionSeconds: &position,
				Tags: []string{}, Frame: &notes.Frame{ID: strings.Repeat("b", 32), DownloadID: strings.Repeat("c", 32), PositionSeconds: position, Approximate: true, Width: 64, Height: 36},
			}
			if _, err := store.db.Exec(`CREATE TRIGGER fail_creation BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.CreateNoteWithFrame(ctx, note, []byte("validated image fixture")); err == nil {
				t.Fatal("failure ignored")
			}
			for _, name := range []string{"notes", "note_frames", "note_creations"} {
				var count int
				if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + name).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial creation", name, count, err)
				}
			}
			if _, err := store.db.Exec(`DROP TRIGGER fail_creation`); err != nil {
				t.Fatal(err)
			}
			saved, err := store.CreateNoteWithFrame(ctx, note, []byte("validated image fixture"))
			if err != nil || saved.Frame == nil {
				t.Fatal("retry after rollback", saved, err)
			}
		})
	}
}

func TestMultiFrameCreationRollsBackEveryImageAndReceipt(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	position := 2.0
	note := notes.Note{
		ID: strings.Repeat("a", 32), CreationHash: "multi-frame-fingerprint",
		ContentID: "youtube:abcdefghijk", Body: "正文", PositionSeconds: &position, Tags: []string{},
	}
	frames := []*notes.Frame{
		{ID: strings.Repeat("b", 32), PositionSeconds: 2, Width: 64, Height: 36, CreatedAt: 1, Source: notes.FrameSourcePlayer},
		{ID: strings.Repeat("c", 32), PositionSeconds: 3, Width: 64, Height: 36, CreatedAt: 1, Source: notes.FrameSourcePlayer},
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_second_frame BEFORE INSERT ON note_frames WHEN NEW.frame_id='` + frames[1].ID + `' BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNoteWithFrames(ctx, note, frames, [][]byte{[]byte("first image"), []byte("second image")}); err == nil {
		t.Fatal("second image failure ignored")
	}
	for _, table := range []string{"notes", "note_frames", "note_creations"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial multi-image creation in %s: %d %v", table, count, err)
		}
	}
	if _, err := store.db.Exec(`DROP TRIGGER fail_second_frame`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNoteWithFrames(ctx, note, frames, [][]byte{[]byte("first image"), []byte("second image")}); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}
