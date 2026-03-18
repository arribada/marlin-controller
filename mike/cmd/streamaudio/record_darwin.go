//go:build darwin

// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"context"
	"io"
	"log"
	"os/exec"
)

func fromMic(output io.Writer) error {

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

	pr, pw := io.Pipe()
	go func() {
		buf := make([]byte, alsaBufferSize2)
		for {
			_, err := pr.Read(buf)
			if err != nil {
				log.Fatal(err)
			}
			workC <- buf
		}
	}()

	cmd := exec.Command("sox", "-d",
		"-t", "wav",
		"-e", "signed-integer",
		"-b", "16",
		"-c", "1",
		"-r", "16000", "-")
	cmd.Stdout = pw
	// cmd.Stderr = os.Stderr
	return cmd.Run()
}
