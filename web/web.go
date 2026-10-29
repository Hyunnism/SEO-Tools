package web

import "embed"

// Assets membungkus berkas antarmuka web dashboard lokal.
//
//go:embed index.html
var Assets embed.FS
