package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:      "gocallview",
		Usage:     "interactively inspect golang module call graphs",
		Action:    doTViewStuff,
		ArgsUsage: "module to start with",
		Flags:     []cli.Flag{},
	}

	file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	log.SetOutput(file)

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}
}
