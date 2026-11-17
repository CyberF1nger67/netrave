// SPDX-License-Identifier: GPL-3.0-only

// Command netrave starts the HTTP service, the supervisor daemon or one of the
// administrative CLI commands.
package main

import (
	"os"

	"netrave/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
