package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// always output wash result to stdout, all others to stderr. Provide flag to adjust verbosity level.
	cqFlg := flag.Int("c", 16, "set networking concurreny limit")
	vFlg := flag.Bool("v", false, "enable verbose mode")
	flag.Usage = usage
	flag.Parse()
	if *cqFlg <= 0 {
		fmt.Println("networking concurrency limit must be positive")
		os.Exit(1)
	}
	log := setUpLog(*vFlg)
	defer log.Sync()
	// default read from stdin. if input file is present, read from input file.
	in := os.Stdin
	if flag.NArg() > 0 {
		infile := flag.Args()[0]
		var err error
		in, err = os.Open(infile)
		if err != nil {
			fmt.Printf("error opening input bookmark file %s: %s\n", infile, err)
			os.Exit(1)
		}
	}
	hc := setupHttpClient( /* TODO: customize timeouts and DNS based on user input */ )
	StartWashTillDone(in, os.Stdout, hc, *cqFlg, log)
}

// customized cli usage
func usage() {
	fmt.Fprint(flag.CommandLine.Output(), `Usage:  mwsh [Options] file

Check if your browser bookmarks are still alive.

Options:
`)
	flag.PrintDefaults()
}

func setUpLog(verbose bool) *zap.SugaredLogger {
	opts := []zap.Option{zap.IncreaseLevel(zapcore.WarnLevel)}
	if verbose {
		opts = append(opts, zap.IncreaseLevel(zapcore.DebugLevel))
	}
	log, err := zap.NewProduction(opts...)
	if err != nil {
		panic(err)
	}
	return log.Sugar()
}

func setupHttpClient() *http.Client {
	return &http.Client{}
}
