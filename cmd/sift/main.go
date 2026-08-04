// Command sift is the v0 CLI (design.md §6, module 8): run a .sift
// program end to end, or inspect it with --emit-ast / --emit-schema.
package main

import (
	"fmt"
	"os"

	"github.com/vitzeno/sift/internal/checker"
	_ "github.com/vitzeno/sift/internal/format" // registers "csv"/"jsonl" via init()
	"github.com/vitzeno/sift/internal/parser"
)

func main() {
	if len(os.Args) != 3 {
		usage()
		os.Exit(2)
	}
	verb, path := os.Args[1], os.Args[2]

	var err error
	switch verb {
	case "run":
		err = runFile(path)
	case "--emit-ast":
		err = emitAST(path)
	case "--emit-schema":
		err = emitSchema(path)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		printErr(path, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sift run <file> | sift --emit-ast <file> | sift --emit-schema <file>")
}

// printErr prints a source position when the error has one (every
// parser/checker error does), and falls back to a plain "file: error:
// message" for anything else (missing file, unregistered format, a
// runtime check failure).
func printErr(path string, err error) {
	switch e := err.(type) {
	case *parser.ParseError:
		fmt.Fprintf(os.Stderr, "%s:%s: error: %s\n", path, e.Pos, e.Msg)
	case *checker.CheckError:
		fmt.Fprintf(os.Stderr, "%s:%s: error: %s\n", path, e.Pos, e.Msg)
	default:
		fmt.Fprintf(os.Stderr, "%s: error: %s\n", path, err)
	}
}
