// Command sift is the v0 CLI (design.md §6, module 8): run a .sift
// program end to end, or inspect it with --emit-ast / --emit-schema.
package main

import (
	"fmt"
	"os"

	"github.com/vitzeno/sift/internal/checker"
	_ "github.com/vitzeno/sift/internal/format/csv"   // registers "csv" via init()
	_ "github.com/vitzeno/sift/internal/format/jsonl" // registers "jsonl" via init()
	_ "github.com/vitzeno/sift/internal/format/xlsx"  // registers "xlsx" via init()
	"github.com/vitzeno/sift/internal/parser"
)

func main() {
	args, print := stripPrintFlag(os.Args[1:])
	if len(args) != 2 {
		usage()
		os.Exit(2)
	}
	verb, path := args[0], args[1]

	var err error
	switch verb {
	case "run":
		err = runFile(path, print)
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

// stripPrintFlag pulls a --print flag out of args, wherever it appears,
// and reports whether it was present. run is the only verb that reads
// it; --emit-ast/--emit-schema just ignore a stray one the same way an
// unrecognized flag would be an error anywhere else -- kept permissive
// here rather than adding a real flag parser for one boolean.
func stripPrintFlag(args []string) (rest []string, print bool) {
	for _, a := range args {
		if a == "--print" {
			print = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, print
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sift run <file> [--print] | sift --emit-ast <file> | sift --emit-schema <file>")
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
