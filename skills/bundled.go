package skills

import (
	"embed"
	"io/fs"
)

const TrackTimeWithBurrowTime = "track-time-with-burrowtime"

//go:embed track-time-with-burrowtime
var bundled embed.FS

// TrackTimeWithBurrowTimeFS returns the files shipped for the BurrowTime agent
// skill, rooted at the skill directory.
func TrackTimeWithBurrowTimeFS() (fs.FS, error) {
	return fs.Sub(bundled, TrackTimeWithBurrowTime)
}
