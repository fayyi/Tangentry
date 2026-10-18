package preferences

// Theme is the user's interface color preference. System resolves against
// the operating system appearance in the web view.
type Theme string

const (
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
	ThemeSystem Theme = "system"
)

func (t Theme) Valid() bool {
	return t == ThemeLight || t == ThemeDark || t == ThemeSystem
}

// ParseTheme normalizes untrusted values to the safe default.
func ParseTheme(value string) Theme {
	theme := Theme(value)
	if theme.Valid() {
		return theme
	}
	return ThemeSystem
}
