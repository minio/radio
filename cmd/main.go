package cmd

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/minio/cli"
	"github.com/minio/mc/pkg/console"
	"github.com/minio/minio/pkg/trie"
	"github.com/minio/minio/pkg/words"
)

// GlobalFlags - global flags for minio.
var GlobalFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "certs-dir, S",
		Value: defaultCertsDir.Get(),
		Usage: "path to certs directory",
	},
	cli.BoolFlag{
		Name:  "quiet",
		Usage: "disable startup information",
	},
	cli.BoolFlag{
		Name:  "anonymous",
		Usage: "hide sensitive information from logging",
	},
	cli.BoolFlag{
		Name:  "json",
		Usage: "output server logs and startup information in json format",
	},
	cli.BoolFlag{
		Name:  "compat",
		Usage: "enable strict S3 compatibility by turning off certain performance optimizations",
	},
}

// Help template for minio.
var minioHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

DESCRIPTION:
  {{.Description}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS] {{end}}COMMAND{{if .VisibleFlags}}{{end}} [ARGS...]

COMMANDS:
  {{range .VisibleCommands}}{{join .Names ", "}}{{ "\t" }}{{.Usage}}
  {{end}}{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
VERSION:
  ` + Version +
	`{{ "\n"}}`

func newApp(name string) *cli.App {
	// Collection of minio commands currently supported are.
	commands := []cli.Command{}

	// Collection of minio commands currently supported in a trie tree.
	commandsTree := trie.NewTrie()

	// registerCommand registers a cli command.
	registerCommand := func(command cli.Command) {
		commands = append(commands, command)
		commandsTree.Insert(command.Name)
	}

	findClosestCommands := func(command string) []string {
		var closestCommands []string
		for _, value := range commandsTree.PrefixMatch(command) {
			closestCommands = append(closestCommands, value.(string))
		}

		sort.Strings(closestCommands)
		// Suggest other close commands - allow missed, wrongly added and
		// even transposed characters
		for _, value := range commandsTree.Walk(commandsTree.Root()) {
			if sort.SearchStrings(closestCommands, value.(string)) < len(closestCommands) {
				continue
			}
			// 2 is arbitrary and represents the max
			// allowed number of typed errors
			if words.DamerauLevenshteinDistance(command, value.(string)) < 2 {
				closestCommands = append(closestCommands, value.(string))
			}
		}

		return closestCommands
	}

	// Register all commands.
	// registerCommand(serverCmd)
	registerCommand(radioCmd)

	// Set up app.
	cli.HelpFlag = cli.BoolFlag{
		Name:  "help, h",
		Usage: "show help",
	}

	app := cli.NewApp()
	app.Name = name
	app.Author = "MinIO, Inc."
	app.Version = Version
	app.Usage = "Perform synchronous replication or erasure coding across object stores"
	app.Description = `RADIO is called Redundant Array of Distributed Independent Objectstores, supports synchronous replication and erasure coding across object storages`
	app.Flags = GlobalFlags
	app.HideHelpCommand = true // Hide `help, h` command, we already have `radio --help`.
	app.Commands = commands
	app.CustomAppHelpTemplate = minioHelpTemplate
	app.CommandNotFound = func(ctx *cli.Context, command string) {
		console.Printf("‘%s’ is not a radio sub-command. See ‘radio --help’.\n", command)
		closestCommands := findClosestCommands(command)
		if len(closestCommands) > 0 {
			console.Println()
			console.Println("Did you mean one of these?")
			for _, cmd := range closestCommands {
				console.Printf("\t‘%s’\n", cmd)
			}
		}

		os.Exit(1)
	}

	return app
}

// Main main for minio server.
func Main(args []string) {
	// Set the minio app name.
	appName := filepath.Base(args[0])

	// Run the app - exit on error.
	if err := newApp(appName).Run(args); err != nil {
		os.Exit(1)
	}
}
