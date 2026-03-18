//go:build !ci

// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"bytes"
	"context"
	"os"
	"testing"
)

const (
	rawAudioFile = "data/audio.dat"
)

/*
GOOS=linux GOARCH=arm64 go test -c -o record.test
scp record.test rpi5:.
./record.test -test.run TestRecord
scp controller:data/audio.dat .
*/
func TestRecordAudio(t *testing.T) {
	rec, err := device()
	if err != nil {
		t.Fatal(err)
	}

	if err = rec.Open(); err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	if err := prepare(rec); err != nil {
		t.Fatal(err)
	}

	var data []byte
	workC := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 10; i++ {
			work := <-workC
			data = append(data, work...)
		}
		cancel()
	}()

	recordAudio(ctx, rec, workC)

	if err := os.MkdirAll("data", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawAudioFile, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessAudio(t *testing.T) {
	data, err := os.ReadFile(rawAudioFile)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	workC := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)
	go func() {
		err = processAudio(ctx, &buf, workC)
		done <- true
	}()

	// send work twice, to make sure reuse of the resampler is alright
	workC <- data
	workC <- data
	cancel()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWav("data/audio.wav", buf.Bytes()); err != nil {
		t.Fatal(err)
	}

	// Now listen to "data/audio.wav" to verify
}
