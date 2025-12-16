package main

import (
	"regexp"
)

var justLetters = regexp.MustCompile(`^\w+$`)
