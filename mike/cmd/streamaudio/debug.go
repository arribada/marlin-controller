// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type debug struct{}

func (db *debug) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(200)

	if r.URL.Path == "/debug/toggle" {
		go toggleStartStop()
		return
	}

	// we don't want to open any of the lines if we're not going to use them (and close them)
	if r.URL.Path != "/debug/red" &&
		r.URL.Path != "/debug/blue" &&
		r.URL.Path != "/debug/green" &&
		r.URL.Path != "/debug/all" &&
		r.URL.Path != "/debug/none" &&
		r.URL.Path != "/debug/blink" {
		return
	}

	gpio, err := newChip()
	if err != nil {
		log.Printf("while creating gpio: %v", err)
		return
	}

	// init the lines
	lines := make(map[int]line)
	for _, v := range []int{redLED, greenLED, blueLED} {
		l, err := gpio.NewLine(v)
		if err != nil {
			log.Printf("while creating rpio line %d: %v", v, err)
			return
		}
		lines[v] = l
	}

	// turn it all off
	for _, v := range lines {
		err := v.SetValue(1)
		if err != nil {
			log.Printf("while setting value: %v", err)
			return
		}
	}

	if r.URL.Path == "/debug/blink" {
		go func() {
			defer func() {
				for _, v := range lines {
					v.Close()
					v.Close()
					v.Close()
				}
			}()
			for _, v := range lines {
				for i := 0; i < 5; i++ {
					err := v.SetValue(0)
					if err != nil {
						log.Printf("cannot set value to 1: %v", err)
						return
					}

					time.Sleep(2 * time.Second)

					err = v.SetValue(1)
					if err != nil {
						log.Printf("cannot set value to 0: %v", err)
						return
					}
					time.Sleep(time.Second)
				}
			}
		}()
		return
	}

	if r.URL.Path == "/debug/all" {
		fmt.Println("all on")
		go func() {
			defer func() {
				for _, v := range lines {
					v.Close()
					v.Close()
					v.Close()
				}
			}()
			for _, v := range lines {
				err := v.SetValue(0)
				if err != nil {
					log.Printf("cannot set value to 1: %v", err)
					return
				}
			}
		}()
		return
	}

	if r.URL.Path == "/debug/none" {
		fmt.Println("all off")
		go func() {
			defer func() {
				for _, v := range lines {
					v.Close()
					v.Close()
					v.Close()
				}
			}()
			for _, v := range lines {
				err := v.SetValue(1)
				if err != nil {
					log.Printf("cannot set value to 0: %v", err)
					return
				}
			}
		}()
		return
	}

	color := strings.TrimPrefix(r.URL.Path, "/debug/")
	colors := make(map[string]int)
	colors["red"], colors["green"], colors["blue"] = redLED, greenLED, blueLED
	clr, ok := colors[color]
	if !ok {
		return
	}
	fmt.Println("turning", color, "on")
	ln := lines[clr]

	go func() {
		defer func() {
			for _, v := range lines {
				v.Close()
				v.Close()
				v.Close()
			}
		}()
		err := ln.SetValue(0)
		if err != nil {
			log.Printf("cannot set value to 1: %v", err)
			return
		}
	}()
}
