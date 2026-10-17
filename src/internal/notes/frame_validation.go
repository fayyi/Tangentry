package notes

import (
	"math"
	"regexp"
)

const maxPlayerFrameSide = 1920

var frameDownloadID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func ValidateFrame(frame *Frame) error {
	if frame == nil {
		return nil
	}
	if frame.Source == FrameSourcePlayer {
		if !noteIDPattern.MatchString(frame.ID) || frame.DownloadID != "" || !validFramePosition(frame.PositionSeconds) ||
			frame.Width < 1 || frame.Height < 1 || frame.Width > maxPlayerFrameSide || frame.Height > maxPlayerFrameSide || frame.CreatedAt < 0 || frame.CreatedAt > 253402300799 {
			return ErrInvalid
		}
		return nil
	}
	if frame.Source != "" || !noteIDPattern.MatchString(frame.ID) || !frameDownloadID.MatchString(frame.DownloadID) ||
		math.IsNaN(frame.PositionSeconds) || math.IsInf(frame.PositionSeconds, 0) || frame.PositionSeconds < 0 || frame.PositionSeconds > 604800 ||
		!frame.Approximate || frame.Width < 1 || frame.Height < 1 || frame.Width > 1080 || frame.Height > 1080 || frame.CreatedAt < 0 || frame.CreatedAt > 253402300799 {
		return ErrInvalid
	}
	return nil
}

func validFramePosition(position float64) bool {
	return !math.IsNaN(position) && !math.IsInf(position, 0) && position >= 0 && position <= 604800
}
