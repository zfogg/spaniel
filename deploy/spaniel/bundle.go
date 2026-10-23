// Package spaniel bundles Spaniel's own monitoring definitions.
package spaniel

import "embed"

// Definitions are shared by deployment mounts and the settings installer.
//go:embed dashboards/*.yaml alerts/*.yaml
var Definitions embed.FS
