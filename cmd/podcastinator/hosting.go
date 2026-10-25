package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Stephan5/podcastinator/internal/hosting"
)

// resolveHosting loads hosting.json from the feed directory, which commands
// that touch the bucket need.
func resolveHosting(feedDirAbs string) (*hosting.Config, error) {
	cfg, err := hosting.Load(feedDirAbs)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("no bucket configured: add %s, or run podcastinator init --bucket <bucket>",
			filepath.Join(feedDirAbs, hosting.Filename))
	}
	return cfg, nil
}

// audioContentType is the Content-Type S3 serves an episode file with.
func audioContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	}
	return ""
}

func isSupportedAudioExt(ext string) bool {
	return audioContentType(ext) != ""
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}
