package main

import (
	"fmt"
	"math/rand"
)

func shout() {
	const red     = 31
	const green   = 32
	const yellow  = 33
	const magenta = 35
	const cyan    = 36
	const white   = 37

	var numbers []int = []int{red,red,red, green,green,green, yellow, magenta,magenta,magenta, cyan,cyan,cyan, white,white}

	var random int = rand.Int()
	var index int = random % len(numbers)
	var number int = numbers[index]

	fmt.Printf("\x1b[%dm", number)

	fmt.Print(`
  ___ _ __   __ _  ___ ___       _ __ ___ (_)_ __ _ __ ___  _ __
 / __| '_ \ / _' |/ __/ _ \_____| '_ ' _ \| | '__| '__/ _ \| '__|
 \__ \ |_) | (_| | (_|  __/_____| | | | | | | |  | | | (_) | |
 |___/ .__/ \__,_|\___\___|     |_| |_| |_|_|_|  |_|  \___/|_|
     |_|

`)
	fmt.Print("\x1b[0m")
}
