package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"tangentry/internal/subtitles"
)

// ArchiveSubtitle retains every distinct acquired version, even when the active
// language is already occupied by an edited or transcribed track.
func (s *Store) ArchiveSubtitle(ctx context.Context, value subtitles.SavedTrack) error {
	raw, err := json.Marshal(value.Track)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO subtitle_archive(content_id,language,fingerprint,payload,saved_at) VALUES(?,?,?,?,?)`, value.ContentID, value.Track.Language, hex.EncodeToString(hash[:]), string(raw), value.SavedAt)
	return err
}
