package main

import "C"

import (
	"bufio"
	"os"
	"fmt"
)

func shouldFetchMoreEntries() bool {
	fmt.Print("Fetch more logs or quit (m/q)? ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadByte()
	return string(input) == "m"
}