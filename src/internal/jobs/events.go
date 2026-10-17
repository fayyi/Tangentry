// Package jobs defines the small runtime event contract shared by long-running
// application work and desktop shells.
package jobs

// Event reports a download or transcription state transition. It deliberately
// contains no domain service types so shells can observe work without knowing
// how the job was implemented.
type Event struct {
	Kind     string
	ID       string
	Title    string
	Active   bool
	Terminal bool
	Outcome  string
}
