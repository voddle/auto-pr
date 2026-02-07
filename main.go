package main

import (
	"fmt"
	"os"

	"auto-pr/internal/cmd"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	args := os.Args[2:]

	switch subcmd {
	case "reviews":
		os.Exit(cmd.RunReviews(args))
	case "reply":
		os.Exit(cmd.RunReply(args))
	case "watch":
		os.Exit(cmd.RunWatch(args))
	case "--help", "-h", "help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", subcmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: auto-pr <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  reviews    Read PR review comments")
	fmt.Println("  reply      Reply to PR review comments")
	fmt.Println("  watch      Auto-watch PR/repo for new reviews and issues")
	fmt.Println()
	fmt.Println("Run 'auto-pr <command> --help' for details on each command.")
}
