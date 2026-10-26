// Package docs embeds public integration documents into release binaries.
package docs

import _ "embed"

//go:embed external-licensing.md
var ExternalLicensingGuide string

//go:embed external-licensing-openapi.json
var ExternalLicensingProtocol string
