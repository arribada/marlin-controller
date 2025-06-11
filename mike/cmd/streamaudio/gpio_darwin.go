//go:build darwin

// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"fmt"
)

const (
	blueLED  = 17
	greenLED = 22
	redLED   = 23
)

func handleButtons() error {
	// NOOP
	return nil
}

func setLED(recording, erroring bool) error {
	if erroring {
		fmt.Println("RED LED ON")
	} else if recording {
		fmt.Println("GREEN LED ON")
	} else {
		fmt.Println("BLUE LED ON")
	}
	return nil
}

func newChip() (chip, error) {
	return mockChip{}, nil
}

type mockChip struct{}

func (c mockChip) NewLine(i int) (line, error) {
	return mockLine{i}, nil
}

type mockLine struct {
	color int
}

func (l mockLine) SetValue(i int) error {
	fmt.Printf("Line %d at %d\n", l.color, i)
	return nil
}

func (l mockLine) Close() error {
	return nil
}
