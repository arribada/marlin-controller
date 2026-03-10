//go:build !darwin

// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/warthog618/gpiod"
	"github.com/warthog618/gpiod/device/rpi"
)

const (
	// TODO: actually do make use of the redLED
	redLED   = rpi.GPIO23
	greenLED = rpi.GPIO22
	blueLED  = rpi.GPIO17
)

func setLED(recording, erroring bool) (ferr error) {
	gpio, err := gpiod.NewChip("gpiochip0")
	if err != nil {
		return fmt.Errorf("while creating gpio: %v", err)
	}

	var color int
	if erroring {
		color = redLED
	} else if recording {
		color = greenLED
	} else {
		color = blueLED
	}

	// N.B: the final color will only be there at the very end of the loop. so
	// there's a brief window where we have a mix of colors.
	for _, v := range []int{redLED, greenLED, blueLED} {
		led, err := gpio.RequestLine(
			v,
			gpiod.AsOutput(0),
			gpiod.WithPullUp,
		)
		if err != nil {
			return fmt.Errorf("while creating rpio line: %v", err)
		}
		defer func(theLED *gpiod.Line) {
			closeErr := theLED.Close()
			if ferr == nil {
				ferr = closeErr
			}
		}(led)
		if v == color {
			err = led.SetValue(0)
		} else {
			err = led.SetValue(1)
		}
		if err != nil {
			return fmt.Errorf("while setting value: %v", err)
		}
	}

	return nil
}

func handleButtons() error {
	gpio, err := gpiod.NewChip("gpiochip0")
	if err != nil {
		return fmt.Errorf("while creating gpio: %v", err)
	}

	line, err := gpio.RequestLine(
		rpi.GPIO6,
		gpiod.WithFallingEdge,
		gpiod.WithDebounce(100*time.Millisecond),
		gpiod.WithEventHandler(handleButtonPress),
	)
	if err != nil {
		return fmt.Errorf("while creating rpio line: %v", err)
	}
	if flagVerbose {
		log.Printf("Listening for button press on GPIO6")
	}

	defer func() {
		if err := line.Close(); err != nil {
			log.Printf("while closing rpio line: %v", err)
		}
	}()

	// TODO: overkill since we probably don't care about closing the lines
	// when the program terminates, do we? in which case handleButtons
	// shouldn't be long lived.
	// But if we keep it, let's add a <-ctx.Done here.
	select {}
}

func handleButtonPress(event gpiod.LineEvent) {
	if flagVerbose {
		log.Printf("Button pressed")
	}

	if event.Type != gpiod.LineEventFallingEdge {
		if flagVerbose {
			log.Printf("Not a falling edge event")
		}
		return
	}
	go func() {
		if err := toggleStartStop(); err != nil {
			log.Printf("while toggling recording: %v", err)
		}
	}()
}

func newChip() (chip, error) {
	c, err := gpiod.NewChip("gpiochip0")
	if err != nil {
		return nil, err
	}
	return realChip{c}, nil
}

type realChip struct {
	gpio *gpiod.Chip
}

func (c realChip) NewLine(i int) (line, error) {
	return c.gpio.RequestLine(i, gpiod.AsOutput(0), gpiod.WithPullUp)
}
