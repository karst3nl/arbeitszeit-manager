// SPDX-License-Identifier: AGPL-3.0-only

// Package web embeds templates and static assets into the binary.
package web

import "embed"

// Templates contains all HTML templates.
//
//go:embed templates/*.html
var Templates embed.FS

// Static contains CSS, JavaScript and vendored libraries.
//
//go:embed static
var Static embed.FS
