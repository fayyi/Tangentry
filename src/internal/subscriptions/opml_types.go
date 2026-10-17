package subscriptions

import "encoding/xml"

const maxOPMLBytes = 1 << 20
const maxOPMLOutlines = 500
const maxOPMLDepth = 64

// OPML is intentionally a small interchange boundary. It carries source
// addresses, labels and folders; importing never fetches a feed or changes a
// refresh policy.
type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr,omitempty"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title string `xml:"title,omitempty"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	XMLName       xml.Name      `xml:"outline"`
	Text          string        `xml:"text,attr,omitempty"`
	Title         string        `xml:"title,attr,omitempty"`
	Type          string        `xml:"type,attr,omitempty"`
	XMLURL        string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL       string        `xml:"htmlUrl,attr,omitempty"`
	TangentryType string        `xml:"tangentryType,attr,omitempty"`
	Children      []opmlOutline `xml:"outline"`
}

type OPMLImportItem struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"` // created, existing, skipped, failed
	Reason  string `json:"reason,omitempty"`
}

type OPMLImportResult struct {
	Created   int              `json:"created"`
	Existing  int              `json:"existing"`
	Skipped   int              `json:"skipped"`
	Failed    int              `json:"failed"`
	Ungrouped int              `json:"ungrouped"`
	Items     []OPMLImportItem `json:"items"`
}
