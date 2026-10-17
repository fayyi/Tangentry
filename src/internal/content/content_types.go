package content

type Item struct {
	Transcripts     []Transcript   `json:"transcripts,omitempty"`
	Images          []Image        `json:"images,omitempty"`
	ArticleBlocks   []ArticleBlock `json:"articleBlocks,omitempty"`
	MediaURL        string         `json:"mediaUrl,omitempty"`
	ID              string         `json:"id"`
	Platform        string         `json:"platform"`
	PlatformID      string         `json:"platformId"`
	Kind            string         `json:"kind"`
	Title           string         `json:"title"`
	Author          string         `json:"author"`
	AuthorID        string         `json:"authorId"`
	AuthorAvatarURL string         `json:"authorAvatarUrl,omitempty"`
	URL             string         `json:"url"`
	ThumbnailURL    string         `json:"thumbnailUrl"`
	Description     string         `json:"description"`
	PreviewText     string         `json:"previewText,omitempty"`
	DurationSeconds int            `json:"durationSeconds"`
	PublishedAt     int64          `json:"publishedAt"`
	PublishedText   string         `json:"publishedText,omitempty"`
	SavedAt         int64          `json:"savedAt,omitempty"`
	// Partial marks a card projection: bodies, transcripts and long
	// descriptions are left out, so it must never be saved back.
	Partial bool `json:"partial,omitempty"`
}

// ArticleBlock is the safe, structured subset of an article that can be
// rendered by the reader. Image URLs are kept separate from text so feeds do
// not have to expose arbitrary HTML to the frontend.
type ArticleBlock struct {
	Kind   string `json:"kind"`
	Level  int    `json:"level,omitempty"`
	Text   string `json:"text"`
	URL    string `json:"url,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type Transcript struct {
	URL      string `json:"url"`
	Type     string `json:"type"`
	Language string `json:"language,omitempty"`
}

// Images preserve album order without tying the library to a platform payload.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Alt    string `json:"alt,omitempty"`
}
