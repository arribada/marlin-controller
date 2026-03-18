//go:build !darwin

// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"context"
	"errors"
	"io"
	"log"
)

func fromMic(output io.Writer) error {
	rec, err := device()
	if err != nil {
		return err
	}

	if err = rec.Open(); err != nil {
		return err
	}
	defer rec.Close()

	if err := prepare(rec); err != nil {
		return err
	}

	// chan size does not really matter here, as long as it's buffered/async.
	workC := make(chan []byte, 100)

	// Asynchronous reception of the audio capture, and then piping it to
	// our audio streamer. We have it async here, so that the main loop on the
	// audio capture can spend most of its time on the audio capture itself,
	// without too much interuption.
	go func() {
		if err := processAudio(context.TODO(), output, workC); err != nil {
			log.Fatal(err)
		}
	}()

	recordAudio(context.TODO(), rec, workC)
	return errors.New("recording unexpectedly stopped")
}
