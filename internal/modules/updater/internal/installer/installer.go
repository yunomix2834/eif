package installer

import "errors"

var ErrUnsupportedPlatform = errors.New("automatic updates are not supported on this platform")
